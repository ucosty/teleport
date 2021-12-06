/*
Copyright 2020 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/gravitational/teleport/api/constants"
	"github.com/gravitational/teleport/api/types"
	apiutils "github.com/gravitational/teleport/api/utils"
	"github.com/gravitational/teleport/lib/auth"
	"github.com/gravitational/teleport/lib/defaults"
	"github.com/gravitational/teleport/lib/reversetunnel"
	"github.com/gravitational/teleport/lib/services"
	"github.com/gravitational/teleport/lib/tlsca"
	"github.com/gravitational/teleport/lib/utils"
	"github.com/sirupsen/logrus"

	"github.com/gravitational/oxy/forward"
	"github.com/gravitational/trace"
)

// transportConfig is configuration for a rewriting transport.
type transportConfig struct {
	proxyClient  reversetunnel.Tunnel
	accessPoint  auth.ReadProxyAccessPoint
	cipherSuites []uint16
	identity     *tlsca.Identity
	servers      []types.AppServer
	ws           types.WebSession
	clusterName  string
	log          *logrus.Entry
}

// Check validates configuration.
func (c *transportConfig) Check() error {
	if c.proxyClient == nil {
		return trace.BadParameter("proxy client missing")
	}
	if c.accessPoint == nil {
		return trace.BadParameter("access point missing")
	}
	if len(c.cipherSuites) == 0 {
		return trace.BadParameter("cipe suites misings")
	}
	if c.identity == nil {
		return trace.BadParameter("identity missing")
	}
	if len(c.servers) == 0 {
		return trace.BadParameter("servers missing")
	}
	if c.ws == nil {
		return trace.BadParameter("web session missing")
	}
	if c.clusterName == "" {
		return trace.BadParameter("cluster name missing")
	}

	return nil
}

// transport is a rewriting http.RoundTripper that can forward requests to
// an application service.
type transport struct {
	mu sync.Mutex
	c  *transportConfig

	// tr is used for forwarding http connections.
	tr http.RoundTripper

	// clientTLSConfig *tls.Config that will be used for mutual authentication.
	clientTLSConfig *tls.Config
}

// newTransport creates a new transport.
func newTransport(c *transportConfig) (*transport, error) {
	var err error
	if err := c.Check(); err != nil {
		return nil, trace.Wrap(err)
	}

	t := &transport{c: c}

	t.clientTLSConfig, err = configureTLS(c)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	// Clone and configure the transport.
	tr, err := defaults.Transport()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	tr.DialContext = t.DialContext
	tr.TLSClientConfig = t.clientTLSConfig

	t.tr = tr
	return t, nil
}

// RoundTrip will rewrite the request, forward the request to the target
// application, emit an event to the audit log, then rewrite the response.
func (t *transport) RoundTrip(r *http.Request) (*http.Response, error) {
	// Perform any request rewriting needed before forwarding the request.
	if err := t.rewriteRequest(r); err != nil {
		return nil, trace.Wrap(err)
	}

	// Forward the request to the target application.
	resp, err := t.tr.RoundTrip(r)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return resp, nil
}

// rewriteRequest applies any rewriting rules to the request before it's forwarded.
func (t *transport) rewriteRequest(r *http.Request) error {
	// Set dummy values for the request forwarder. Dialing through the tunnel is
	// actually performed using the transport created for this session but these
	// are needed for the forwarder.
	r.URL.Scheme = "https"
	r.URL.Host = constants.APIDomain

	// Don't trust any "X-Forward-*" headers the client sends, instead set own and then
	// forward request.
	headers := &forward.HeaderRewriter{
		TrustForwardHeader: false,
	}
	headers.Rewrite(r)

	// Remove the application session cookie from the header. This is done by
	// first wiping out the "Cookie" header then adding back all cookies
	// except the application session cookie. This appears to be the safest way
	// to serialize cookies.
	cookies := r.Cookies()
	r.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name == CookieName || cookie.Name == AuthStateCookieName {
			continue
		}
		r.AddCookie(cookie)
	}

	return nil
}

// DialContext
func (t *transport) DialContext(ctx context.Context, _ string, _ string) (net.Conn, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i := len(t.c.servers) - 1; i >= 0; i-- {
		conn, err := dialAppServer(t.c.proxyClient, t.c.identity, t.c.servers[i])
		if err != nil {
			// Connection problem with the server.
			if trace.IsConnectionProblem(err) {
				t.c.log.Warnf("failed to connect to app server: %v.", err)

				// remove the failed server from the list.
				t.c.servers = append(t.c.servers[:i], t.c.servers[i+1:]...)
				continue
			}

			return nil, trace.Wrap(err)
		}

		return conn, nil
	}

	return nil, trace.ConnectionProblem(fmt.Errorf("no servers remaining to connect"), "")
}

// DialWebsocket dials a websocket connection over the transport's reverse
// tunnel.
func (t *transport) DialWebsocket(network, address string) (net.Conn, error) {
	conn, err := t.DialContext(context.Background(), network, address)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	// App access connections over reverse tunnel use mutual TLS.
	return tls.Client(conn, t.clientTLSConfig), nil
}

// dialAppServer dial and connect to the application service over the reverse
// tunnel subsystem.
func dialAppServer(proxyClient reversetunnel.Tunnel, identity *tlsca.Identity, server types.AppServer) (net.Conn, error) {
	clusterClient, err := proxyClient.GetSite(identity.RouteToApp.ClusterName)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	conn, err := clusterClient.Dial(reversetunnel.DialParams{
		From:     &utils.NetAddr{AddrNetwork: "tcp", Addr: "@web-proxy"},
		To:       &utils.NetAddr{AddrNetwork: "tcp", Addr: reversetunnel.LocalNode},
		ServerID: fmt.Sprintf("%v.%v", server.GetHostID(), identity.RouteToApp.ClusterName),
		ConnType: types.AppTunnel,
	})
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return conn, nil
}

// configureTLS creates and configures a *tls.Config that will be used for
// mutual authentication.
func configureTLS(c *transportConfig) (*tls.Config, error) {
	tlsConfig := utils.TLSConfig(c.cipherSuites)

	// Configure the pool of certificates that will be used to verify the
	// identity of the server. This allows the client to verify the identity of
	// the server it is connecting to.
	ca, err := c.accessPoint.GetCertAuthority(types.CertAuthID{
		Type:       types.HostCA,
		DomainName: c.identity.RouteToApp.ClusterName,
	}, false)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	certPool, err := services.CertPool(ca)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	tlsConfig.RootCAs = certPool

	// Configure the identity that will be used to connect to the server. This
	// allows the server to verify the identity of the caller.
	certificate, err := tls.X509KeyPair(c.ws.GetTLSCert(), c.ws.GetPriv())
	if err != nil {
		return nil, trace.Wrap(err, "failed to parse certificate or key")
	}
	tlsConfig.Certificates = []tls.Certificate{certificate}

	// Use SNI to tell the other side which cluster signed the CA so it doesn't
	// have to fetch all CAs when verifying the cert.
	tlsConfig.ServerName = apiutils.EncodeClusterName(c.clusterName)

	return tlsConfig, nil
}
