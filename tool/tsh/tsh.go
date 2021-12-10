/*
Copyright 2016-2021 Gravitational, Inc.

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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/gravitational/teleport"
	"github.com/gravitational/teleport/api/constants"
	apidefaults "github.com/gravitational/teleport/api/defaults"
	"github.com/gravitational/teleport/api/types"
	apisshutils "github.com/gravitational/teleport/api/utils/sshutils"
	"github.com/gravitational/teleport/lib/asciitable"
	"github.com/gravitational/teleport/lib/auth"
	"github.com/gravitational/teleport/lib/benchmark"
	"github.com/gravitational/teleport/lib/client"
	dbprofile "github.com/gravitational/teleport/lib/client/db"
	"github.com/gravitational/teleport/lib/client/identityfile"
	"github.com/gravitational/teleport/lib/defaults"
	"github.com/gravitational/teleport/lib/events"
	"github.com/gravitational/teleport/lib/kube/kubeconfig"
	"github.com/gravitational/teleport/lib/modules"
	"github.com/gravitational/teleport/lib/services"
	"github.com/gravitational/teleport/lib/session"
	"github.com/gravitational/teleport/lib/sshutils"
	"github.com/gravitational/teleport/lib/sshutils/scp"
	"github.com/gravitational/teleport/lib/tlsca"
	"github.com/gravitational/teleport/lib/utils"

	"github.com/gravitational/kingpin"
	"github.com/gravitational/trace"

	gops "github.com/google/gops/agent"
	"github.com/jonboulle/clockwork"
	"github.com/sirupsen/logrus"
)

var log = logrus.WithFields(logrus.Fields{
	trace.Component: teleport.ComponentTSH,
})

// CLIConf stores command line arguments and flags:
type CLIConf struct {
	// UserHost contains "[login]@hostname" argument to SSH command
	UserHost string
	// Commands to execute on a remote host
	RemoteCommand []string
	// DesiredRoles indicates one or more roles which should be requested.
	DesiredRoles string
	// RequestReason indicates the reason for an access request.
	RequestReason string
	// SuggestedReviewers is a list of suggested request reviewers.
	SuggestedReviewers string
	// NoWait can be used with an access request to exit without waiting for a request resolution.
	NoWait bool
	// RequestID is an access request ID
	RequestID string
	// ReviewReason indicates the reason for an access review.
	ReviewReason string
	// ReviewableRequests indicates that only requests which can be reviewed should
	// be listed.
	ReviewableRequests bool
	// SuggestedRequests indicates that only requests which suggest the current user
	// as a reviewer should be listed.
	SuggestedRequests bool
	// MyRequests indicates that only requests created by the current user
	// should be listed.
	MyRequests bool
	// Approve/Deny indicates the desired review kind.
	Approve, Deny bool
	// Username is the Teleport user's username (to login into proxies)
	Username string
	// Proxy keeps the hostname:port of the SSH proxy to use
	Proxy string
	// TTL defines how long a session must be active (in minutes)
	MinsToLive int32
	// SSH Port on a remote SSH host
	NodePort int32
	// Login on a remote SSH host
	NodeLogin string
	// InsecureSkipVerify bypasses verification of HTTPS certificate when talking to web proxy
	InsecureSkipVerify bool
	// Remote SSH session to join
	SessionID string
	// Src:dest parameter for SCP
	CopySpec []string
	// -r flag for scp
	RecursiveCopy bool
	// -L flag for ssh. Local port forwarding like 'ssh -L 80:remote.host:80 -L 443:remote.host:443'
	LocalForwardPorts []string
	// DynamicForwardedPorts is port forwarding using SOCKS5. It is similar to
	// "ssh -D 8080 example.com".
	DynamicForwardedPorts []string
	// ForwardAgent agent to target node. Equivalent of -A for OpenSSH.
	ForwardAgent bool
	// ProxyJump is an optional -J flag pointing to the list of jumphosts,
	// it is an equivalent of --proxy flag in tsh interpretation
	ProxyJump string
	// --local flag for ssh
	LocalExec bool
	// SiteName specifies remote site go login to
	SiteName string
	// KubernetesCluster specifies the kubernetes cluster to login to.
	KubernetesCluster string
	// DatabaseService specifies the database proxy server to log into.
	DatabaseService string
	// DatabaseUser specifies database user to embed in the certificate.
	DatabaseUser string
	// DatabaseName specifies database name to embed in the certificate.
	DatabaseName string
	// AppName specifies proxied application name.
	AppName string
	// Interactive, when set to true, launches remote command with the terminal attached
	Interactive bool
	// Quiet mode, -q command (disables progress printing)
	Quiet bool
	// Namespace is used to select cluster namespace
	Namespace string
	// NoCache is used to turn off client cache for nodes discovery
	NoCache bool
	// BenchDuration is a duration for the benchmark
	BenchDuration time.Duration
	// BenchRate is a requests per second rate to mantain
	BenchRate int
	// BenchInteractive indicates that we should create interactive session
	BenchInteractive bool
	// BenchExport exports the latency profile
	BenchExport bool
	// BenchExportPath saves the latency profile in provided path
	BenchExportPath string
	// BenchTicks ticks per half distance
	BenchTicks int32
	// BenchValueScale value at which to scale the values recorded
	BenchValueScale float64
	// Context is a context to control execution
	Context context.Context
	// Gops starts gops agent on a specified address
	// if not specified, gops won't start
	Gops bool
	// GopsAddr specifies to gops addr to listen on
	GopsAddr string
	// IdentityFileIn is an argument to -i flag (path to the private key+cert file)
	IdentityFileIn string
	// Compatibility flags, --compat, specifies OpenSSH compatibility flags.
	Compatibility string
	// CertificateFormat defines the format of the user SSH certificate.
	CertificateFormat string
	// IdentityFileOut is an argument to -out flag
	IdentityFileOut string
	// IdentityFormat (used for --format flag for 'tsh login') defines which
	// format to use with --out to store a fershly retreived certificate
	IdentityFormat identityfile.Format
	// IdentityOverwrite when true will overwrite any existing identity file at
	// IdentityFileOut. When false, user will be prompted before overwriting
	// any files.
	IdentityOverwrite bool

	// BindAddr is an address in the form of host:port to bind to
	// during `tsh login` command
	BindAddr string

	// AuthConnector is the name of the connector to use.
	AuthConnector string

	// SkipVersionCheck skips version checking for client and server
	SkipVersionCheck bool

	// Options is a list of OpenSSH options in the format used in the
	// configuration file.
	Options []string

	// Verbose is used to print extra output.
	Verbose bool

	// Format is used to change the format of output
	Format string

	// NoRemoteExec will not execute a remote command after connecting to a host,
	// will block instead. Useful when port forwarding. Equivalent of -N for OpenSSH.
	NoRemoteExec bool

	// X11Forwarding will set up X11 forwarding for the session ('ssh -X')
	X11Forwarding bool

	// X11Forwarding will set up trusted X11 forwarding for the session ('ssh -Y')
	X11ForwardingTrusted bool

	// Debug sends debug logs to stdout.
	Debug bool

	// Browser can be used to pass the name of a browser to override the system default
	// (not currently implemented), or set to 'none' to suppress browser opening entirely.
	Browser string

	// UseLocalSSHAgent set to false will prevent this client from attempting to
	// connect to the local ssh-agent (or similar) socket at $SSH_AUTH_SOCK.
	//
	// Deprecated in favor of `AddKeysToAgent`.
	UseLocalSSHAgent bool

	// AddKeysToAgent specifies the behaviour of how certs are handled.
	AddKeysToAgent string

	// EnableEscapeSequences will scan stdin for SSH escape sequences during
	// command/shell execution. This also requires stdin to be an interactive
	// terminal.
	EnableEscapeSequences bool

	// PreserveAttrs preserves access/modification times from the original file.
	PreserveAttrs bool

	// executablePath is the absolute path to the current executable.
	executablePath string

	// unsetEnvironment unsets Teleport related environment variables.
	unsetEnvironment bool

	// mockSSOLogin used in tests to override sso login handler in teleport client.
	mockSSOLogin client.SSOLoginFunc

	// HomePath is where tsh stores profiles
	HomePath string

	// LocalProxyPort is a port used by local proxy listener.
	LocalProxyPort string

	// ConfigProxyTarget is the node which should be connected to in `tsh config-proxy`.
	ConfigProxyTarget string

	// AWSRole is Amazon Role ARN or role name that will be used for AWS CLI access.
	AWSRole string
	// AWSCommandArgs contains arguments that will be forwarded to AWS CLI binary.
	AWSCommandArgs []string
}

func main() {
	cmdLineOrig := os.Args[1:]
	var cmdLine []string

	// lets see: if the executable name is 'ssh' or 'scp' we convert
	// that to "tsh ssh" or "tsh scp"
	switch path.Base(os.Args[0]) {
	case "ssh":
		cmdLine = append([]string{"ssh"}, cmdLineOrig...)
	case "scp":
		cmdLine = append([]string{"scp"}, cmdLineOrig...)
	default:
		cmdLine = cmdLineOrig
	}
	if err := Run(cmdLine); err != nil {
		utils.FatalError(err)
	}
}

const (
	authEnvVar        = "TELEPORT_AUTH"
	clusterEnvVar     = "TELEPORT_CLUSTER"
	kubeClusterEnvVar = "TELEPORT_KUBE_CLUSTER"
	loginEnvVar       = "TELEPORT_LOGIN"
	bindAddrEnvVar    = "TELEPORT_LOGIN_BIND_ADDR"
	proxyEnvVar       = "TELEPORT_PROXY"
	homeEnvVar        = "TELEPORT_HOME"
	// TELEPORT_SITE uses the older deprecated "site" terminology to refer to a
	// cluster. All new code should use TELEPORT_CLUSTER instead.
	siteEnvVar             = "TELEPORT_SITE"
	userEnvVar             = "TELEPORT_USER"
	addKeysToAgentEnvVar   = "TELEPORT_ADD_KEYS_TO_AGENT"
	useLocalSSHAgentEnvVar = "TELEPORT_USE_LOCAL_SSH_AGENT"

	clusterHelp = "Specify the Teleport cluster to connect"
	browserHelp = "Set to 'none' to suppress browser opening on login"

	// proxyDefaultResolutionTimeout is how long to wait for an unknown proxy
	// port to be resolved.
	//
	// Originally based on the RFC-8305 "Maximum Connection Attempt Delay"
	// recommended default value of 2s. In the RFC this value is for the
	// establishment of a TCP connection, rather than the full HTTP round-
	// trip that we measure against, so some tweaking may be needed.
	proxyDefaultResolutionTimeout = 2 * time.Second
)

// cliOption is used in tests to inject/override configuration within Run
type cliOption func(*CLIConf) error

// Run executes TSH client. same as main() but easier to test
func Run(args []string, opts ...cliOption) error {
	var cf CLIConf
	utils.InitLogger(utils.LoggingForCLI, logrus.WarnLevel)

	moduleCfg := modules.GetModules()

	// configure CLI argument parser:
	app := utils.InitCLIParser("tsh", "TSH: Teleport Authentication Gateway Client").Interspersed(false)
	app.Flag("login", "Remote host login").Short('l').Envar(loginEnvVar).StringVar(&cf.NodeLogin)
	localUser, _ := client.Username()
	app.Flag("proxy", "SSH proxy address").Envar(proxyEnvVar).StringVar(&cf.Proxy)
	app.Flag("nocache", "do not cache cluster discovery locally").Hidden().BoolVar(&cf.NoCache)
	app.Flag("user", fmt.Sprintf("SSH proxy user [%s]", localUser)).Envar(userEnvVar).StringVar(&cf.Username)
	app.Flag("option", "").Short('o').Hidden().AllowDuplicate().PreAction(func(ctx *kingpin.ParseContext) error {
		return trace.BadParameter("invalid flag, perhaps you want to use this flag as tsh ssh -o?")
	}).String()

	app.Flag("ttl", "Minutes to live for a SSH session").Int32Var(&cf.MinsToLive)
	app.Flag("identity", "Identity file").Short('i').StringVar(&cf.IdentityFileIn)
	app.Flag("compat", "OpenSSH compatibility flag").Hidden().StringVar(&cf.Compatibility)
	app.Flag("cert-format", "SSH certificate format").StringVar(&cf.CertificateFormat)

	if !moduleCfg.IsBoringBinary() {
		// The user is *never* allowed to do this in FIPS mode.
		app.Flag("insecure", "Do not verify server's certificate and host name. Use only in test environments").
			Default("false").
			BoolVar(&cf.InsecureSkipVerify)
	}

	app.Flag("auth", "Specify the type of authentication connector to use.").Envar(authEnvVar).StringVar(&cf.AuthConnector)
	app.Flag("namespace", "Namespace of the cluster").Default(apidefaults.Namespace).Hidden().StringVar(&cf.Namespace)
	app.Flag("gops", "Start gops endpoint on a given address").Hidden().BoolVar(&cf.Gops)
	app.Flag("gops-addr", "Specify gops addr to listen on").Hidden().StringVar(&cf.GopsAddr)
	app.Flag("skip-version-check", "Skip version checking between server and client.").BoolVar(&cf.SkipVersionCheck)
	app.Flag("debug", "Verbose logging to stdout").Short('d').BoolVar(&cf.Debug)
	app.Flag("add-keys-to-agent", fmt.Sprintf("Controls how keys are handled. Valid values are %v.", client.AllAddKeysOptions)).Short('k').Envar(addKeysToAgentEnvVar).Default(client.AddKeysToAgentAuto).StringVar(&cf.AddKeysToAgent)
	app.Flag("use-local-ssh-agent", "Deprecated in favor of the add-keys-to-agent flag.").
		Hidden().
		Envar(useLocalSSHAgentEnvVar).
		Default("true").
		BoolVar(&cf.UseLocalSSHAgent)
	app.Flag("enable-escape-sequences", "Enable support for SSH escape sequences. Type '~?' during an SSH session to list supported sequences. Default is enabled.").
		Default("true").
		BoolVar(&cf.EnableEscapeSequences)
	app.Flag("bind-addr", "Override host:port used when opening a browser for cluster logins").Envar(bindAddrEnvVar).StringVar(&cf.BindAddr)
	app.HelpFlag.Short('h')
	ver := app.Command("version", "Print the version")
	// ssh
	ssh := app.Command("ssh", "Run shell or execute a command on a remote SSH node")
	ssh.Arg("[user@]host", "Remote hostname and the login to use").Required().StringVar(&cf.UserHost)
	ssh.Arg("command", "Command to execute on a remote host").StringsVar(&cf.RemoteCommand)
	app.Flag("jumphost", "SSH jumphost").Short('J').StringVar(&cf.ProxyJump)
	ssh.Flag("port", "SSH port on a remote host").Short('p').Int32Var(&cf.NodePort)
	ssh.Flag("forward-agent", "Forward agent to target node").Short('A').BoolVar(&cf.ForwardAgent)
	ssh.Flag("forward", "Forward localhost connections to remote server").Short('L').StringsVar(&cf.LocalForwardPorts)
	ssh.Flag("dynamic-forward", "Forward localhost connections to remote server using SOCKS5").Short('D').StringsVar(&cf.DynamicForwardedPorts)
	ssh.Flag("local", "Execute command on localhost after connecting to SSH node").Default("false").BoolVar(&cf.LocalExec)
	ssh.Flag("tty", "Allocate TTY").Short('t').BoolVar(&cf.Interactive)
	ssh.Flag("cluster", clusterHelp).StringVar(&cf.SiteName)
	ssh.Flag("option", "OpenSSH options in the format used in the configuration file").Short('o').AllowDuplicate().StringsVar(&cf.Options)
	ssh.Flag("no-remote-exec", "Don't execute remote command, useful for port forwarding").Short('N').BoolVar(&cf.NoRemoteExec)
	ssh.Flag("X", "Setup x11 forwarding in untrusted mode (secure) for this request").Short('X').BoolVar(&cf.X11Forwarding)
	ssh.Flag("Y", "Setup x11 forwarding in trusted mode (insecure) for this request").Short('Y').Default("true").BoolVar(&cf.X11ForwardingTrusted)

	// AWS.
	aws := app.Command("aws", "Access AWS API.")
	aws.Arg("command", "AWS command and subcommands arguments that are going to be forwarded to AWS CLI").StringsVar(&cf.AWSCommandArgs)
	aws.Flag("app", "Optional Name of the AWS application to use if logged into multiple.").StringVar(&cf.AppName)

	// Applications.
	apps := app.Command("apps", "View and control proxied applications.").Alias("app")
	lsApps := apps.Command("ls", "List available applications.")
	lsApps.Flag("verbose", "Show extra application fields.").Short('v').BoolVar(&cf.Verbose)
	lsApps.Flag("cluster", clusterHelp).StringVar(&cf.SiteName)
	appLogin := apps.Command("login", "Retrieve short-lived certificate for an app.")
	appLogin.Arg("app", "App name to retrieve credentials for. Can be obtained from `tsh apps ls` output.").Required().StringVar(&cf.AppName)
	appLogin.Flag("aws-role", "(For AWS CLI access only) Amazon IAM role ARN or role name.").StringVar(&cf.AWSRole)
	appLogout := apps.Command("logout", "Remove app certificate.")
	appLogout.Arg("app", "App to remove credentials for.").StringVar(&cf.AppName)
	appConfig := apps.Command("config", "Print app connection information.")
	appConfig.Arg("app", "App to print information for. Required when logged into multiple apps.").StringVar(&cf.AppName)
	appConfig.Flag("format", fmt.Sprintf("Optional print format, one of: %q to print app address, %q to print CA cert path, %q to print cert path, %q print key path, %q to print example curl command.",
		appFormatURI, appFormatCA, appFormatCert, appFormatKey, appFormatCURL)).StringVar(&cf.Format)

	// Local TLS proxy.
	proxy := app.Command("proxy", "Run local TLS proxy allowing connecting to Teleport in single-port mode")
	proxySSH := proxy.Command("ssh", "Start local TLS proxy for ssh connections when using Teleport in single-port mode")
	proxySSH.Arg("[user@]host", "Remote hostname and the login to use").Required().StringVar(&cf.UserHost)
	proxySSH.Flag("cluster", clusterHelp).StringVar(&cf.SiteName)
	proxyDB := proxy.Command("db", "Start local TLS proxy for database connections when using Teleport in single-port mode")
	proxyDB.Arg("db", "The name of the database to start local proxy for").Required().StringVar(&cf.DatabaseService)
	proxyDB.Flag("port", " Specifies the source port used by proxy db listener").Short('p').StringVar(&cf.LocalProxyPort)

	// Databases.
	db := app.Command("db", "View and control proxied databases.")
	dbList := db.Command("ls", "List all available databases.")
	dbList.Flag("verbose", "Show extra database fields.").Short('v').BoolVar(&cf.Verbose)
	dbList.Flag("cluster", clusterHelp).StringVar(&cf.SiteName)
	dbLogin := db.Command("login", "Retrieve credentials for a database.")
	dbLogin.Arg("db", "Database to retrieve credentials for. Can be obtained from 'tsh db ls' output.").Required().StringVar(&cf.DatabaseService)
	dbLogin.Flag("db-user", "Optional database user to configure as default.").StringVar(&cf.DatabaseUser)
	dbLogin.Flag("db-name", "Optional database name to configure as default.").StringVar(&cf.DatabaseName)
	dbLogout := db.Command("logout", "Remove database credentials.")
	dbLogout.Arg("db", "Database to remove credentials for.").StringVar(&cf.DatabaseService)
	dbEnv := db.Command("env", "Print environment variables for the configured database.")
	dbEnv.Arg("db", "Print environment for the specified database").StringVar(&cf.DatabaseService)
	// --db flag is deprecated in favor of positional argument for consistency with other commands.
	dbEnv.Flag("db", "Print environment for the specified database.").Hidden().StringVar(&cf.DatabaseService)
	dbConfig := db.Command("config", "Print database connection information. Useful when configuring GUI clients.")
	dbConfig.Arg("db", "Print information for the specified database.").StringVar(&cf.DatabaseService)
	// --db flag is deprecated in favor of positional argument for consistency with other commands.
	dbConfig.Flag("db", "Print information for the specified database.").Hidden().StringVar(&cf.DatabaseService)
	dbConfig.Flag("format", fmt.Sprintf("Print format: %q to print in table format (default), %q to print connect command.", dbFormatText, dbFormatCommand)).StringVar(&cf.Format)
	dbConnect := db.Command("connect", "Connect to a database.")
	dbConnect.Arg("db", "Database service name to connect to.").StringVar(&cf.DatabaseService)
	dbConnect.Flag("db-user", "Optional database user to log in as.").StringVar(&cf.DatabaseUser)
	dbConnect.Flag("db-name", "Optional database name to log in to.").StringVar(&cf.DatabaseName)

	// join
	join := app.Command("join", "Join the active SSH session")
	join.Flag("cluster", clusterHelp).StringVar(&cf.SiteName)
	join.Arg("session-id", "ID of the session to join").Required().StringVar(&cf.SessionID)
	// play
	play := app.Command("play", "Replay the recorded SSH session")
	play.Flag("cluster", clusterHelp).StringVar(&cf.SiteName)
	play.Flag("format", "Format output (json, pty)").Short('f').Default(teleport.PTY).StringVar(&cf.Format)
	play.Arg("session-id", "ID of the session to play").Required().StringVar(&cf.SessionID)

	// scp
	scp := app.Command("scp", "Secure file copy")
	scp.Flag("cluster", clusterHelp).StringVar(&cf.SiteName)
	scp.Arg("from, to", "Source and destination to copy").Required().StringsVar(&cf.CopySpec)
	scp.Flag("recursive", "Recursive copy of subdirectories").Short('r').BoolVar(&cf.RecursiveCopy)
	scp.Flag("port", "Port to connect to on the remote host").Short('P').Int32Var(&cf.NodePort)
	scp.Flag("preserve", "Preserves access and modification times from the original file").Short('p').BoolVar(&cf.PreserveAttrs)
	scp.Flag("quiet", "Quiet mode").Short('q').BoolVar(&cf.Quiet)
	// ls
	ls := app.Command("ls", "List remote SSH nodes")
	ls.Flag("cluster", clusterHelp).StringVar(&cf.SiteName)
	ls.Arg("labels", "List of labels to filter node list").StringVar(&cf.UserHost)
	ls.Flag("verbose", "One-line output (for text format), including node UUIDs").Short('v').BoolVar(&cf.Verbose)
	ls.Flag("format", "Format output (text, json, names)").Short('f').Default(teleport.Text).StringVar(&cf.Format)
	// clusters
	clusters := app.Command("clusters", "List available Teleport clusters")
	clusters.Flag("quiet", "Quiet mode").Short('q').BoolVar(&cf.Quiet)

	// login logs in with remote proxy and obtains a "session certificate" which gets
	// stored in ~/.tsh directory
	login := app.Command("login", "Log in to a cluster and retrieve the session certificate")
	login.Flag("out", "Identity output").Short('o').AllowDuplicate().StringVar(&cf.IdentityFileOut)
	login.Flag("format", fmt.Sprintf("Identity format: %s, %s (for OpenSSH compatibility) or %s (for kubeconfig)",
		identityfile.DefaultFormat,
		identityfile.FormatOpenSSH,
		identityfile.FormatKubernetes,
	)).Default(string(identityfile.DefaultFormat)).StringVar((*string)(&cf.IdentityFormat))
	login.Flag("overwrite", "Whether to overwrite the existing identity file.").BoolVar(&cf.IdentityOverwrite)
	login.Flag("request-roles", "Request one or more extra roles").StringVar(&cf.DesiredRoles)
	login.Flag("request-reason", "Reason for requesting additional roles").StringVar(&cf.RequestReason)
	login.Flag("request-reviewers", "Suggested reviewers for role request").StringVar(&cf.SuggestedReviewers)
	login.Flag("request-nowait", "Finish without waiting for request resolution").BoolVar(&cf.NoWait)
	login.Flag("request-id", "Login with the roles requested in the given request").StringVar(&cf.RequestID)
	login.Arg("cluster", clusterHelp).StringVar(&cf.SiteName)
	login.Flag("browser", browserHelp).StringVar(&cf.Browser)
	login.Flag("kube-cluster", "Name of the Kubernetes cluster to login to").StringVar(&cf.KubernetesCluster)
	login.Alias(loginUsageFooter)

	// logout deletes obtained session certificates in ~/.tsh
	logout := app.Command("logout", "Delete a cluster certificate")

	// bench
	bench := app.Command("bench", "Run shell or execute a command on a remote SSH node").Hidden()
	bench.Flag("cluster", clusterHelp).StringVar(&cf.SiteName)
	bench.Arg("[user@]host", "Remote hostname and the login to use").Required().StringVar(&cf.UserHost)
	bench.Arg("command", "Command to execute on a remote host").Required().StringsVar(&cf.RemoteCommand)
	bench.Flag("port", "SSH port on a remote host").Short('p').Int32Var(&cf.NodePort)
	bench.Flag("duration", "Test duration").Default("1s").DurationVar(&cf.BenchDuration)
	bench.Flag("rate", "Requests per second rate").Default("10").IntVar(&cf.BenchRate)
	bench.Flag("interactive", "Create interactive SSH session").BoolVar(&cf.BenchInteractive)
	bench.Flag("export", "Export the latency profile").BoolVar(&cf.BenchExport)
	bench.Flag("path", "Directory to save the latency profile to, default path is the current directory").Default(".").StringVar(&cf.BenchExportPath)
	bench.Flag("ticks", "Ticks per half distance").Default("100").Int32Var(&cf.BenchTicks)
	bench.Flag("scale", "Value scale in which to scale the recorded values").Default("1.0").Float64Var(&cf.BenchValueScale)

	// show key
	show := app.Command("show", "Read an identity from file and print to stdout").Hidden()
	show.Arg("identity_file", "The file containing a public key or a certificate").Required().StringVar(&cf.IdentityFileIn)

	// The status command shows which proxy the user is logged into and metadata
	// about the certificate.
	status := app.Command("status", "Display the list of proxy servers and retrieved certificates")

	// The environment command prints out environment variables for the configured
	// proxy and cluster. Can be used to create sessions "sticky" to a terminal
	// even if the user runs "tsh login" again in another window.
	environment := app.Command("env", "Print commands to set Teleport session environment variables")
	environment.Flag("unset", "Print commands to clear Teleport session environment variables").BoolVar(&cf.unsetEnvironment)

	req := app.Command("request", "Manage access requests").Alias("requests")

	reqList := req.Command("ls", "List access requests").Alias("list")
	reqList.Flag("format", "Format output (text, json)").Short('f').Default(teleport.Text).StringVar(&cf.Format)
	reqList.Flag("reviewable", "Only show requests reviewable by current user").BoolVar(&cf.ReviewableRequests)
	reqList.Flag("suggested", "Only show requests that suggest current user as reviewer").BoolVar(&cf.SuggestedRequests)
	reqList.Flag("my-requests", "Only show requests created by current user").BoolVar(&cf.MyRequests)

	reqShow := req.Command("show", "Show request details").Alias("details")
	reqShow.Arg("request-id", "ID of the target request").Required().StringVar(&cf.RequestID)

	reqCreate := req.Command("new", "Create a new access request").Alias("create")
	reqCreate.Flag("roles", "Roles to be requested").Required().StringVar(&cf.DesiredRoles)
	reqCreate.Flag("reason", "Reason for requesting").StringVar(&cf.RequestReason)
	reqCreate.Flag("reviewers", "Suggested reviewers").StringVar(&cf.SuggestedReviewers)
	reqCreate.Flag("nowait", "Finish without waiting for request resolution").BoolVar(&cf.NoWait)

	reqReview := req.Command("review", "Review an access request")
	reqReview.Arg("request-id", "ID of target request").Required().StringVar(&cf.RequestID)
	reqReview.Flag("approve", "Review proposes approval").BoolVar(&cf.Approve)
	reqReview.Flag("deny", "Review proposes denial").BoolVar(&cf.Deny)
	reqReview.Flag("reason", "Review reason message").StringVar(&cf.ReviewReason)

	// Kubernetes subcommands.
	kube := newKubeCommand(app)
	// MFA subcommands.
	mfa := newMFACommand(app)

	config := app.Command("config", "Print OpenSSH configuration details")

	// config-proxy is a wrapper to ensure Windows clients can properly use
	// `tsh config`. As it's not intended to run by users directly and may
	// not have a stable CLI interface, hide it.
	// DELETE IN 9.0: The config-proxy logic was moved to the tsh proxy ssh command.
	configProxy := app.Command("config-proxy", "ProxyCommand wrapper for SSH config as generated by `config`").Hidden()
	configProxy.Arg("target", "Target node host:port").Required().StringVar(&cf.ConfigProxyTarget)
	configProxy.Arg("cluster-name", "Target cluster name").Required().StringVar(&cf.SiteName)

	// On Windows, hide the "ssh", "join", "play", "scp", and "bench" commands
	// because they all use a terminal.
	if runtime.GOOS == constants.WindowsOS {
		bench.Hidden()
	}

	// parse CLI commands+flags:
	command, err := app.Parse(args)
	if err != nil {
		return trace.Wrap(err)
	}

	// apply any options after parsing of arguments to ensure
	// that defaults don't overwrite options.
	for _, opt := range opts {
		if err := opt(&cf); err != nil {
			return trace.Wrap(err)
		}
	}

	// While in debug mode, send logs to stdout.
	if cf.Debug {
		utils.InitLogger(utils.LoggingForCLI, logrus.DebugLevel)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		exitSignals := make(chan os.Signal, 1)
		signal.Notify(exitSignals, syscall.SIGTERM, syscall.SIGINT)

		sig := <-exitSignals
		log.Debugf("signal: %v", sig)
		cancel()
	}()
	cf.Context = ctx

	if cf.Gops {
		log.Debugf("Starting gops agent.")
		err = gops.Listen(gops.Options{Addr: cf.GopsAddr})
		if err != nil {
			log.Warningf("Failed to start gops agent %v.", err)
		}
	}

	cf.executablePath, err = os.Executable()
	if err != nil {
		return trace.Wrap(err)
	}

	if err := client.ValidateAgentKeyOption(cf.AddKeysToAgent); err != nil {
		return trace.Wrap(err)
	}

	setEnvFlags(&cf, os.Getenv)

	switch command {
	case ver.FullCommand():
		utils.PrintVersion()
	case ssh.FullCommand():
		err = onSSH(&cf)
	case bench.FullCommand():
		err = onBenchmark(&cf)
	case join.FullCommand():
		err = onJoin(&cf)
	case scp.FullCommand():
		err = onSCP(&cf)
	case play.FullCommand():
		err = onPlay(&cf)
	case ls.FullCommand():
		err = onListNodes(&cf)
	case clusters.FullCommand():
		err = onListClusters(&cf)
	case login.FullCommand():
		err = onLogin(&cf)
	case logout.FullCommand():
		if err := refuseArgs(logout.FullCommand(), args); err != nil {
			return trace.Wrap(err)
		}
		err = onLogout(&cf)
	case show.FullCommand():
		err = onShow(&cf)
	case status.FullCommand():
		err = onStatus(&cf)
	case lsApps.FullCommand():
		err = onApps(&cf)
	case appLogin.FullCommand():
		err = onAppLogin(&cf)
	case appLogout.FullCommand():
		err = onAppLogout(&cf)
	case appConfig.FullCommand():
		err = onAppConfig(&cf)
	case kube.credentials.FullCommand():
		err = kube.credentials.run(&cf)
	case kube.ls.FullCommand():
		err = kube.ls.run(&cf)
	case kube.login.FullCommand():
		err = kube.login.run(&cf)

	case proxySSH.FullCommand():
		err = onProxyCommandSSH(&cf)
	case proxyDB.FullCommand():
		err = onProxyCommandDB(&cf)

	case dbList.FullCommand():
		err = onListDatabases(&cf)
	case dbLogin.FullCommand():
		err = onDatabaseLogin(&cf)
	case dbLogout.FullCommand():
		err = onDatabaseLogout(&cf)
	case dbEnv.FullCommand():
		err = onDatabaseEnv(&cf)
	case dbConfig.FullCommand():
		err = onDatabaseConfig(&cf)
	case dbConnect.FullCommand():
		err = onDatabaseConnect(&cf)
	case environment.FullCommand():
		err = onEnvironment(&cf)
	case mfa.ls.FullCommand():
		err = mfa.ls.run(&cf)
	case mfa.add.FullCommand():
		err = mfa.add.run(&cf)
	case mfa.rm.FullCommand():
		err = mfa.rm.run(&cf)
	case reqList.FullCommand():
		err = onRequestList(&cf)
	case reqShow.FullCommand():
		err = onRequestShow(&cf)
	case reqCreate.FullCommand():
		err = onRequestCreate(&cf)
	case reqReview.FullCommand():
		err = onRequestReview(&cf)
	case config.FullCommand():
		err = onConfig(&cf)
	case configProxy.FullCommand():
		err = onConfigProxy(&cf)
	case aws.FullCommand():
		err = onAWS(&cf)
	default:
		// This should only happen when there's a missing switch case above.
		err = trace.BadParameter("command %q not configured", command)
	}

	if trace.IsNotImplemented(err) {
		return handleUnimplementedError(ctx, err, cf)
	}

	return trace.Wrap(err)
}

// onPlay replays a session with a given ID
func onPlay(cf *CLIConf) error {
	switch cf.Format {
	case teleport.PTY:
		switch {
		case path.Ext(cf.SessionID) == ".tar":
			sid := sessionIDFromPath(cf.SessionID)
			tarFile, err := os.Open(cf.SessionID)
			defer tarFile.Close()
			if err != nil {
				return trace.ConvertSystemError(err)
			}
			if err := client.PlayFile(context.TODO(), tarFile, sid); err != nil {
				return trace.Wrap(err)
			}
		default:
			tc, err := makeClient(cf, true)
			if err != nil {
				return trace.Wrap(err)
			}
			if err := tc.Play(context.TODO(), cf.Namespace, cf.SessionID); err != nil {
				return trace.Wrap(err)
			}
		}
	default:
		err := exportFile(cf.SessionID, cf.Format)
		if err != nil {
			return trace.Wrap(err)
		}
	}
	return nil
}

func sessionIDFromPath(path string) string {
	fileName := filepath.Base(path)
	return strings.TrimSuffix(fileName, ".tar")
}

func exportFile(path string, format string) error {
	f, err := os.Open(path)
	if err != nil {
		return trace.ConvertSystemError(err)
	}
	defer f.Close()
	err = events.Export(context.TODO(), f, os.Stdout, format)
	if err != nil {
		return trace.Wrap(err)
	}
	return nil
}

// onLogin logs in with remote proxy and gets signed certificates
func onLogin(cf *CLIConf) error {
	autoRequest := true
	// special case: --request-roles=no disables auto-request behavior.
	if cf.DesiredRoles == "no" {
		autoRequest = false
		cf.DesiredRoles = ""
	}

	if cf.IdentityFileIn != "" {
		return trace.BadParameter("-i flag cannot be used here")
	}

	switch cf.IdentityFormat {
	case identityfile.FormatFile, identityfile.FormatOpenSSH, identityfile.FormatKubernetes:
	default:
		return trace.BadParameter("invalid identity format: %s", cf.IdentityFormat)
	}

	// Get the status of the active profile as well as the status
	// of any other proxies the user is logged into.
	profile, profiles, err := client.Status(cf.HomePath, cf.Proxy)
	if err != nil {
		if !trace.IsNotFound(err) {
			return trace.Wrap(err)
		}
	}

	// make the teleport client and retrieve the certificate from the proxy:
	tc, err := makeClient(cf, true)
	if err != nil {
		return trace.Wrap(err)
	}
	tc.HomePath = cf.HomePath
	// client is already logged in and profile is not expired
	if profile != nil && !profile.IsExpired(clockwork.NewRealClock()) {
		switch {
		// in case if nothing is specified, re-fetch kube clusters and print
		// current status
		case cf.Proxy == "" && cf.SiteName == "" && cf.DesiredRoles == "" && cf.RequestID == "" && cf.IdentityFileOut == "":
			if err := updateKubeConfig(cf, tc, ""); err != nil {
				return trace.Wrap(err)
			}
			printProfiles(cf.Debug, profile, profiles)
			return nil
		// in case if parameters match, re-fetch kube clusters and print
		// current status
		case host(cf.Proxy) == host(profile.ProxyURL.Host) && cf.SiteName == profile.Cluster && cf.DesiredRoles == "" && cf.RequestID == "":
			if err := updateKubeConfig(cf, tc, ""); err != nil {
				return trace.Wrap(err)
			}
			printProfiles(cf.Debug, profile, profiles)
			return nil
		// proxy is unspecified or the same as the currently provided proxy,
		// but cluster is specified, treat this as selecting a new cluster
		// for the same proxy
		case (cf.Proxy == "" || host(cf.Proxy) == host(profile.ProxyURL.Host)) && cf.SiteName != "":
			// trigger reissue, preserving any active requests.
			err = tc.ReissueUserCerts(cf.Context, client.CertCacheKeep, client.ReissueParams{
				AccessRequests: profile.ActiveRequests.AccessRequests,
				RouteToCluster: cf.SiteName,
			})
			if err != nil {
				return trace.Wrap(err)
			}
			if err := tc.SaveProfile(cf.HomePath, true); err != nil {
				return trace.Wrap(err)
			}
			if err := updateKubeConfig(cf, tc, ""); err != nil {
				return trace.Wrap(err)
			}
			return trace.Wrap(onStatus(cf))
		// proxy is unspecified or the same as the currently provided proxy,
		// but desired roles or request ID is specified, treat this as a
		// privilege escalation request for the same login session.
		case (cf.Proxy == "" || host(cf.Proxy) == host(profile.ProxyURL.Host)) && (cf.DesiredRoles != "" || cf.RequestID != "") && cf.IdentityFileOut == "":
			if err := executeAccessRequest(cf, tc); err != nil {
				return trace.Wrap(err)
			}
			if err := updateKubeConfig(cf, tc, ""); err != nil {
				return trace.Wrap(err)
			}
			return trace.Wrap(onStatus(cf))
		// otherwise just passthrough to standard login
		default:
		}
	}

	if cf.Username == "" {
		cf.Username = tc.Username
	}

	// -i flag specified? save the retrieved cert into an identity file
	makeIdentityFile := (cf.IdentityFileOut != "")

	key, err := tc.Login(cf.Context)
	if err != nil {
		return trace.Wrap(err)
	}

	// the login operation may update the username and should be considered the more
	// "authoritative" source.
	cf.Username = tc.Username

	// TODO(fspmarshall): Refactor access request & cert reissue logic to allow
	// access requests to be applied to identity files.

	if makeIdentityFile {
		if err := setupNoninteractiveClient(tc, key); err != nil {
			return trace.Wrap(err)
		}
		// key.TrustedCA at this point only has the CA of the root cluster we
		// logged into. We need to fetch all the CAs for leaf clusters too, to
		// make them available in the identity file.
		rootClusterName := key.TrustedCA[0].ClusterName
		authorities, err := tc.GetTrustedCA(cf.Context, rootClusterName)
		if err != nil {
			return trace.Wrap(err)
		}
		key.TrustedCA = auth.AuthoritiesToTrustedCerts(authorities)

		filesWritten, err := identityfile.Write(identityfile.WriteConfig{
			OutputPath:           cf.IdentityFileOut,
			Key:                  key,
			Format:               cf.IdentityFormat,
			KubeProxyAddr:        tc.KubeClusterAddr(),
			OverwriteDestination: cf.IdentityOverwrite,
		})
		if err != nil {
			return trace.Wrap(err)
		}
		fmt.Printf("\nThe certificate has been written to %s\n", strings.Join(filesWritten, ","))
		return nil
	}

	if err := tc.ActivateKey(cf.Context, key); err != nil {
		return trace.Wrap(err)
	}

	// If the proxy is advertising that it supports Kubernetes, update kubeconfig.
	if tc.KubeProxyAddr != "" {
		if err := updateKubeConfig(cf, tc, ""); err != nil {
			return trace.Wrap(err)
		}
	}

	// Regular login without -i flag.
	if err := tc.SaveProfile(cf.HomePath, true); err != nil {
		return trace.Wrap(err)
	}

	if autoRequest && cf.DesiredRoles == "" && cf.RequestID == "" {
		var requireReason, auto bool
		var prompt string
		roleNames, err := key.CertRoles()
		if err != nil {
			logoutErr := tc.Logout()
			return trace.NewAggregate(err, logoutErr)
		}
		// load all roles from root cluster and collect relevant options.
		// the normal one-off TeleportClient methods don't re-use the auth server
		// connection, so we use WithRootClusterClient to speed things up.
		err = tc.WithRootClusterClient(cf.Context, func(clt auth.ClientI) error {
			for _, roleName := range roleNames {
				role, err := clt.GetRole(cf.Context, roleName)
				if err != nil {
					return trace.Wrap(err)
				}
				requireReason = requireReason || role.GetOptions().RequestAccess.RequireReason()
				auto = auto || role.GetOptions().RequestAccess.ShouldAutoRequest()
				if prompt == "" {
					prompt = role.GetOptions().RequestPrompt
				}
			}
			return nil
		})
		if err != nil {
			logoutErr := tc.Logout()
			return trace.NewAggregate(err, logoutErr)
		}
		if requireReason && cf.RequestReason == "" {
			msg := "--request-reason must be specified"
			if prompt != "" {
				msg = msg + ", prompt=" + prompt
			}
			err := trace.BadParameter(msg)
			logoutErr := tc.Logout()
			return trace.NewAggregate(err, logoutErr)
		}
		if auto {
			cf.DesiredRoles = "*"
		}
	}

	if cf.DesiredRoles != "" || cf.RequestID != "" {
		fmt.Println("") // visually separate access request output
		if err := executeAccessRequest(cf, tc); err != nil {
			logoutErr := tc.Logout()
			return trace.NewAggregate(err, logoutErr)
		}
	}

	// Update the command line flag for the proxy to make sure any advertised
	// settings are picked up.
	webProxyHost, _ := tc.WebProxyHostPort()
	cf.Proxy = webProxyHost

	// Print status to show information of the logged in user.
	return trace.Wrap(onStatus(cf))
}

// setupNoninteractiveClient sets up existing client to use
// non-interactive authentication methods
func setupNoninteractiveClient(tc *client.TeleportClient, key *client.Key) error {
	certUsername, err := key.CertUsername()
	if err != nil {
		return trace.Wrap(err)
	}
	tc.Username = certUsername

	// Extract and set the HostLogin to be the first principal. It doesn't
	// matter what the value is, but some valid principal has to be set
	// otherwise the certificate won't be validated.
	certPrincipals, err := key.CertPrincipals()
	if err != nil {
		return trace.Wrap(err)
	}
	if len(certPrincipals) == 0 {
		return trace.BadParameter("no principals found")
	}
	tc.HostLogin = certPrincipals[0]

	identityAuth, err := authFromIdentity(key)
	if err != nil {
		return trace.Wrap(err)
	}
	tc.TLS, err = key.TeleportClientTLSConfig(nil)
	if err != nil {
		return trace.Wrap(err)
	}
	tc.AuthMethods = []ssh.AuthMethod{identityAuth}
	tc.Interactive = false
	tc.SkipLocalAuth = true

	// When user logs in for the first time without a CA in ~/.tsh/known_hosts,
	// and specifies the -out flag, we need to avoid writing anything to
	// ~/.tsh/ but still validate the proxy cert. Because the existing
	// client.Client methods have a side-effect of persisting the CA on disk,
	// we do all of this by hand.
	//
	// Wrap tc.HostKeyCallback with a another checker. This outer checker uses
	// key.TrustedCA to validate the remote host cert first, before falling
	// back to the original HostKeyCallback.
	oldHostKeyCallback := tc.HostKeyCallback
	tc.HostKeyCallback = func(hostname string, remote net.Addr, hostKey ssh.PublicKey) error {
		checker := ssh.CertChecker{
			// ssh.CertChecker will parse hostKey, extract public key of the
			// signer (CA) and call IsHostAuthority. IsHostAuthority in turn
			// has to match hostCAKey to any known trusted CA.
			IsHostAuthority: func(hostCAKey ssh.PublicKey, address string) bool {
				for _, ca := range key.TrustedCA {
					caKeys, err := ca.SSHCertPublicKeys()
					if err != nil {
						return false
					}
					for _, caKey := range caKeys {
						if apisshutils.KeysEqual(caKey, hostCAKey) {
							return true
						}
					}
				}
				return false
			},
		}
		err := checker.CheckHostKey(hostname, remote, hostKey)
		if err != nil {
			if oldHostKeyCallback == nil {
				return trace.Wrap(err)
			}
			errOld := oldHostKeyCallback(hostname, remote, hostKey)
			if errOld != nil {
				return trace.NewAggregate(err, errOld)
			}
		}
		return nil
	}
	return nil
}

// onLogout deletes a "session certificate" from ~/.tsh for a given proxy
func onLogout(cf *CLIConf) error {
	// Extract all clusters the user is currently logged into.
	active, available, err := client.Status(cf.HomePath, "")
	if err != nil {
		if trace.IsNotFound(err) {
			fmt.Printf("All users logged out.\n")
			return nil
		} else if trace.IsAccessDenied(err) {
			fmt.Printf("%v: Logged in user does not have the correct permissions\n", err)
			return nil
		}
		return trace.Wrap(err)
	}
	profiles := append([]*client.ProfileStatus{}, available...)
	if active != nil {
		profiles = append(profiles, active)
	}

	// Extract the proxy name.
	proxyHost, _, err := net.SplitHostPort(cf.Proxy)
	if err != nil {
		proxyHost = cf.Proxy
	}

	switch {
	// Proxy and username for key to remove.
	case proxyHost != "" && cf.Username != "":
		tc, err := makeClient(cf, true)
		if err != nil {
			return trace.Wrap(err)
		}

		// Load profile for the requested proxy/user.
		profile, err := client.StatusFor(cf.HomePath, proxyHost, cf.Username)
		if err != nil && !trace.IsNotFound(err) {
			return trace.Wrap(err)
		}

		// Log out user from the databases.
		if profile != nil {
			for _, db := range profile.Databases {
				log.Debugf("Logging %v out of database %v.", profile.Name, db)
				err = dbprofile.Delete(tc, db)
				if err != nil {
					return trace.Wrap(err)
				}
			}
		}

		// Remove keys for this user from disk and running agent.
		err = tc.Logout()
		if err != nil {
			if trace.IsNotFound(err) {
				fmt.Printf("User %v already logged out from %v.\n", cf.Username, proxyHost)
				os.Exit(1)
			}
			return trace.Wrap(err)
		}

		// Get the address of the active Kubernetes proxy to find AuthInfos,
		// Clusters, and Contexts in kubeconfig.
		clusterName, _ := tc.KubeProxyHostPort()
		if tc.SiteName != "" {
			clusterName = fmt.Sprintf("%v.%v", tc.SiteName, clusterName)
		}

		// Remove Teleport related entries from kubeconfig.
		log.Debugf("Removing Teleport related entries for '%v' from kubeconfig.", clusterName)
		err = kubeconfig.Remove("", clusterName)
		if err != nil {
			return trace.Wrap(err)
		}

		fmt.Printf("Logged out %v from %v.\n", cf.Username, proxyHost)
	// Remove all keys.
	case proxyHost == "" && cf.Username == "":
		// The makeClient function requires a proxy. However this value is not used
		// because the user will be logged out from all proxies. Pass a dummy value
		// to allow creation of the TeleportClient.
		cf.Proxy = "dummy:1234"
		tc, err := makeClient(cf, true)
		if err != nil {
			return trace.Wrap(err)
		}

		// Remove Teleport related entries from kubeconfig for all clusters.
		for _, profile := range profiles {
			log.Debugf("Removing Teleport related entries for '%v' from kubeconfig.", profile.Cluster)
			err = kubeconfig.Remove("", profile.Cluster)
			if err != nil {
				return trace.Wrap(err)
			}
		}

		// Remove all database access related profiles as well such as Postgres
		// connection service file.
		for _, profile := range profiles {
			for _, db := range profile.Databases {
				log.Debugf("Logging %v out of database %v.", profile.Name, db)
				err = dbprofile.Delete(tc, db)
				if err != nil {
					return trace.Wrap(err)
				}
			}
		}

		// Remove all keys from disk and the running agent.
		err = tc.LogoutAll()
		if err != nil {
			return trace.Wrap(err)
		}

		fmt.Printf("Logged out all users from all proxies.\n")
	default:
		fmt.Printf("Specify --proxy and --user to remove keys for specific user ")
		fmt.Printf("from a proxy or neither to log out all users from all proxies.\n")
	}
	return nil
}

// onListNodes executes 'tsh ls' command.
func onListNodes(cf *CLIConf) error {
	tc, err := makeClient(cf, true)
	if err != nil {
		return trace.Wrap(err)
	}

	// Get list of all nodes in backend and sort by "Node Name".
	var nodes []types.Server
	err = client.RetryWithRelogin(cf.Context, tc, func() error {
		nodes, err = tc.ListNodes(cf.Context)
		return err
	})
	if err != nil {
		return trace.Wrap(err)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].GetHostname() < nodes[j].GetHostname()
	})

	if err := printNodes(nodes, cf.Format, cf.Verbose); err != nil {
		return trace.Wrap(err)
	}

	return nil
}

func executeAccessRequest(cf *CLIConf, tc *client.TeleportClient) error {
	if cf.DesiredRoles == "" && cf.RequestID == "" {
		return trace.BadParameter("at least one role or a request ID must be specified")
	}
	if cf.Username == "" {
		cf.Username = tc.Username
	}

	var req types.AccessRequest
	var err error
	if cf.RequestID != "" {
		err = tc.WithRootClusterClient(cf.Context, func(clt auth.ClientI) error {
			reqs, err := clt.GetAccessRequests(cf.Context, types.AccessRequestFilter{
				ID:   cf.RequestID,
				User: cf.Username,
			})
			if err != nil {
				return trace.Wrap(err)
			}
			if len(reqs) != 1 {
				return trace.BadParameter(`invalid access request "%v"`, cf.RequestID)
			}
			req = reqs[0]
			return nil
		})
		if err != nil {
			return trace.Wrap(err)
		}

		// If the request isn't pending, handle resolution
		if !req.GetState().IsPending() {
			err := onRequestResolution(cf, tc, req)
			return trace.Wrap(err)
		}

		fmt.Fprint(os.Stdout, "Request pending...\n")
	} else {
		roles := utils.SplitIdentifiers(cf.DesiredRoles)
		reviewers := utils.SplitIdentifiers(cf.SuggestedReviewers)
		req, err = services.NewAccessRequest(cf.Username, roles...)
		if err != nil {
			return trace.Wrap(err)
		}
		req.SetRequestReason(cf.RequestReason)
		req.SetSuggestedReviewers(reviewers)
	}

	// Watch for resolution events on the given request. Start watcher before
	// creating the request to avoid a potential race.
	errChan := make(chan error)
	if !cf.NoWait {
		go func() {
			errChan <- waitForRequestResolution(cf, tc, req)
		}()
	}

	// Create request if it doesn't already exist
	if cf.RequestID == "" {
		cf.RequestID = req.GetName()
		fmt.Fprint(os.Stdout, "Creating request...\n")
		// always create access request against the root cluster
		if err = tc.WithRootClusterClient(cf.Context, func(clt auth.ClientI) error {
			err := clt.CreateAccessRequest(cf.Context, req)
			return trace.Wrap(err)
		}); err != nil {
			return trace.Wrap(err)
		}
	}

	onRequestShow(cf)
	fmt.Println("")

	// Dont wait for request to get resolved, just print out request info
	if cf.NoWait {
		return nil
	}

	// Wait for watcher to return
	fmt.Fprintf(os.Stdout, "Waiting for request approval...\n")
	return trace.Wrap(<-errChan)
}

func printNodes(nodes []types.Server, format string, verbose bool) error {
	switch strings.ToLower(format) {
	case teleport.Text:
		printNodesAsText(nodes, verbose)
	case teleport.JSON:
		out, err := json.MarshalIndent(nodes, "", "  ")
		if err != nil {
			return trace.Wrap(err)
		}
		fmt.Println(string(out))
	case teleport.Names:
		for _, n := range nodes {
			fmt.Println(n.GetHostname())
		}
	default:
		return trace.BadParameter("unsupported format. try 'json', 'text', or 'names'")
	}

	return nil
}

func printNodesAsText(nodes []types.Server, verbose bool) {
	// Reusable function to get addr or tunnel for each node
	getAddr := func(n types.Server) string {
		if n.GetUseTunnel() {
			return "⟵ Tunnel"
		}
		return n.GetAddr()
	}

	var t asciitable.Table
	switch verbose {
	// In verbose mode, print everything on a single line and include the Node
	// ID (UUID). Useful for machines that need to parse the output of "tsh ls".
	case true:
		t = asciitable.MakeTable([]string{"Node Name", "Node ID", "Address", "Labels"})
		for _, n := range nodes {
			t.AddRow([]string{
				n.GetHostname(), n.GetName(), getAddr(n), n.LabelsString(),
			})
		}
	// In normal mode chunk the labels and print two per line and allow multiple
	// lines per node.
	case false:
		t = asciitable.MakeTable([]string{"Node Name", "Address", "Labels"})
		for _, n := range nodes {
			labelChunks := chunkLabels(n.GetAllLabels(), 2)
			for i, v := range labelChunks {
				if i == 0 {
					t.AddRow([]string{n.GetHostname(), getAddr(n), strings.Join(v, ", ")})
				} else {
					t.AddRow([]string{"", "", strings.Join(v, ", ")})
				}
			}
		}
	}

	fmt.Println(t.AsBuffer().String())
}

func showApps(apps []types.Application, active []tlsca.RouteToApp, verbose bool) {
	// In verbose mode, print everything on a single line and include host UUID.
	// In normal mode, chunk the labels, print two per line and allow multiple
	// lines per node.
	if verbose {
		t := asciitable.MakeTable([]string{"Application", "Description", "Public Address", "URI", "Labels"})
		for _, app := range apps {
			name := app.GetName()
			for _, a := range active {
				if name == a.Name {
					name = fmt.Sprintf("> %v", name)
				}
			}
			t.AddRow([]string{
				name,
				app.GetDescription(),
				app.GetPublicAddr(),
				app.GetURI(),
				app.LabelsString(),
			})
		}
		fmt.Println(t.AsBuffer().String())
	} else {
		t := asciitable.MakeTable([]string{"Application", "Description", "Public Address", "Labels"})
		for _, app := range apps {
			labelChunks := chunkLabels(app.GetAllLabels(), 2)
			for i, v := range labelChunks {
				var name string
				var addr string
				if i == 0 {
					name = app.GetName()
					addr = app.GetPublicAddr()
				}
				for _, a := range active {
					if name == a.Name {
						name = fmt.Sprintf("> %v", name)
					}
				}
				t.AddRow([]string{
					name,
					app.GetDescription(),
					addr,
					strings.Join(v, ", "),
				})
			}
		}
		fmt.Println(t.AsBuffer().String())
	}
}

func showDatabases(cluster string, databases []types.Database, active []tlsca.RouteToDatabase, verbose bool) {
	if verbose {
		t := asciitable.MakeTable([]string{"Name", "Description", "Protocol", "Type", "URI", "Labels", "Connect", "Expires"})
		for _, database := range databases {
			name := database.GetName()
			var connect string
			for _, a := range active {
				if a.ServiceName == name {
					name = formatActiveDB(a)
					connect = formatConnectCommand(cluster, a)
				}
			}
			t.AddRow([]string{
				name,
				database.GetDescription(),
				database.GetProtocol(),
				database.GetType(),
				database.GetURI(),
				database.LabelsString(),
				connect,
				database.Expiry().Format(constants.HumanDateFormatSeconds),
			})
		}
		fmt.Println(t.AsBuffer().String())
	} else {
		t := asciitable.MakeTable([]string{"Name", "Description", "Labels", "Connect"})
		for _, database := range databases {
			name := database.GetName()
			var connect string
			for _, a := range active {
				if a.ServiceName == name {
					name = formatActiveDB(a)
					connect = formatConnectCommand(cluster, a)
				}
			}
			t.AddRow([]string{
				name,
				database.GetDescription(),
				formatDatabaseLabels(database),
				connect,
			})
		}
		fmt.Println(t.AsBuffer().String())
	}
}

func formatDatabaseLabels(database types.Database) string {
	labels := database.GetAllLabels()
	// Hide the origin label unless printing verbose table.
	delete(labels, types.OriginLabel)
	return types.LabelsAsString(labels, nil)
}

// formatConnectCommand formats an appropriate database connection command
// for a user based on the provided database parameters.
func formatConnectCommand(cluster string, active tlsca.RouteToDatabase) string {
	switch {
	case active.Username != "" && active.Database != "":
		return fmt.Sprintf("tsh db connect %v", active.ServiceName)
	case active.Username != "":
		return fmt.Sprintf("tsh db connect --db-name=<name> %v", active.ServiceName)
	case active.Database != "":
		return fmt.Sprintf("tsh db connect --db-user=<user> %v", active.ServiceName)
	}
	return fmt.Sprintf("tsh db connect --db-user=<user> --db-name=<name> %v", active.ServiceName)
}

func formatActiveDB(active tlsca.RouteToDatabase) string {
	switch {
	case active.Username != "" && active.Database != "":
		return fmt.Sprintf("> %v (user: %v, db: %v)", active.ServiceName, active.Username, active.Database)
	case active.Username != "":
		return fmt.Sprintf("> %v (user: %v)", active.ServiceName, active.Username)
	case active.Database != "":
		return fmt.Sprintf("> %v (db: %v)", active.ServiceName, active.Database)
	}
	return fmt.Sprintf("> %v", active.ServiceName)
}

// chunkLabels breaks labels into sized chunks. Used to improve readability
// of "tsh ls".
func chunkLabels(labels map[string]string, chunkSize int) [][]string {
	// First sort labels so they always occur in the same order.
	sorted := make([]string, 0, len(labels))
	for k, v := range labels {
		sorted = append(sorted, fmt.Sprintf("%v=%v", k, v))
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Then chunk labels into sized chunks.
	var chunks [][]string
	for chunkSize < len(sorted) {
		sorted, chunks = sorted[chunkSize:], append(chunks, sorted[0:chunkSize:chunkSize])
	}
	chunks = append(chunks, sorted)

	return chunks
}

// onListClusters executes 'tsh clusters' command
func onListClusters(cf *CLIConf) error {
	tc, err := makeClient(cf, true)
	if err != nil {
		return trace.Wrap(err)
	}

	var rootClusterName string
	var leafClusters []types.RemoteCluster
	err = client.RetryWithRelogin(cf.Context, tc, func() error {
		proxyClient, err := tc.ConnectToProxy(cf.Context)
		if err != nil {
			return err
		}
		defer proxyClient.Close()

		var rootErr, leafErr error
		rootClusterName, rootErr = proxyClient.RootClusterName()
		leafClusters, leafErr = proxyClient.GetLeafClusters(cf.Context)
		return trace.NewAggregate(rootErr, leafErr)
	})
	if err != nil {
		return trace.Wrap(err)
	}

	profile, _, err := client.Status(cf.HomePath, cf.Proxy)
	if err != nil {
		return trace.Wrap(err)
	}
	showSelected := func(clusterName string) string {
		if profile != nil && clusterName == profile.Cluster {
			return "*"
		}
		return ""
	}

	var t asciitable.Table
	if cf.Quiet {
		t = asciitable.MakeHeadlessTable(4)
	} else {
		t = asciitable.MakeTable([]string{"Cluster Name", "Status", "Cluster Type", "Selected"})
	}

	t.AddRow([]string{
		rootClusterName, teleport.RemoteClusterStatusOnline, "root", showSelected(rootClusterName),
	})
	for _, cluster := range leafClusters {
		t.AddRow([]string{
			cluster.GetName(), cluster.GetConnectionStatus(), "leaf", showSelected(cluster.GetName()),
		})
	}
	fmt.Println(t.AsBuffer().String())
	return nil
}

// onSSH executes 'tsh ssh' command
func onSSH(cf *CLIConf) error {
	tc, err := makeClient(cf, false)
	if err != nil {
		return trace.Wrap(err)
	}

	tc.Stdin = os.Stdin
	err = client.RetryWithRelogin(cf.Context, tc, func() error {
		return tc.SSH(cf.Context, cf.RemoteCommand, cf.LocalExec)
	})
	if err != nil {
		if strings.Contains(utils.UserMessageFromError(err), teleport.NodeIsAmbiguous) {
			allNodes, err := tc.ListAllNodes(cf.Context)
			if err != nil {
				return trace.Wrap(err)
			}
			var nodes []types.Server
			for _, node := range allNodes {
				if node.GetHostname() == tc.Host {
					nodes = append(nodes, node)
				}
			}
			fmt.Fprintf(os.Stderr, "error: ambiguous host could match multiple nodes\n\n")
			printNodesAsText(nodes, true)
			fmt.Fprintf(os.Stderr, "Hint: try addressing the node by unique id (ex: tsh ssh user@node-id)\n")
			fmt.Fprintf(os.Stderr, "Hint: use 'tsh ls -v' to list all nodes with their unique ids\n")
			fmt.Fprintf(os.Stderr, "\n")
			os.Exit(1)
		}
		// exit with the same exit status as the failed command:
		if tc.ExitStatus != 0 {
			fmt.Fprintln(os.Stderr, utils.UserMessageFromError(err))
			os.Exit(tc.ExitStatus)
		} else {
			return trace.Wrap(err)
		}
	}
	return nil
}

// onBenchmark executes benchmark
func onBenchmark(cf *CLIConf) error {
	tc, err := makeClient(cf, false)
	if err != nil {
		return trace.Wrap(err)
	}
	cnf := benchmark.Config{
		Command:       cf.RemoteCommand,
		MinimumWindow: cf.BenchDuration,
		Rate:          cf.BenchRate,
	}
	result, err := cnf.Benchmark(cf.Context, tc)
	if err != nil {
		fmt.Fprintln(os.Stderr, utils.UserMessageFromError(err))
		os.Exit(255)
	}
	fmt.Printf("\n")
	fmt.Printf("* Requests originated: %v\n", result.RequestsOriginated)
	fmt.Printf("* Requests failed: %v\n", result.RequestsFailed)
	if result.LastError != nil {
		fmt.Printf("* Last error: %v\n", result.LastError)
	}
	fmt.Printf("\nHistogram\n\n")
	t := asciitable.MakeTable([]string{"Percentile", "Response Duration"})
	for _, quantile := range []float64{25, 50, 75, 90, 95, 99, 100} {
		t.AddRow([]string{fmt.Sprintf("%v", quantile),
			fmt.Sprintf("%v ms", result.Histogram.ValueAtQuantile(quantile)),
		})
	}
	if _, err := io.Copy(os.Stdout, t.AsBuffer()); err != nil {
		return trace.Wrap(err)
	}
	fmt.Printf("\n")
	if cf.BenchExport {
		path, err := benchmark.ExportLatencyProfile(cf.BenchExportPath, result.Histogram, cf.BenchTicks, cf.BenchValueScale)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed exporting latency profile: %s\n", utils.UserMessageFromError(err))
		} else {
			fmt.Printf("latency profile saved: %v\n", path)
		}
	}
	return nil
}

// onJoin executes 'ssh join' command
func onJoin(cf *CLIConf) error {
	tc, err := makeClient(cf, true)
	if err != nil {
		return trace.Wrap(err)
	}
	sid, err := session.ParseID(cf.SessionID)
	if err != nil {
		return trace.BadParameter("'%v' is not a valid session ID (must be GUID)", cf.SessionID)
	}
	err = client.RetryWithRelogin(cf.Context, tc, func() error {
		return tc.Join(context.TODO(), cf.Namespace, *sid, nil)
	})
	if err != nil {
		return trace.Wrap(err)
	}
	return nil
}

// onSCP executes 'tsh scp' command
func onSCP(cf *CLIConf) error {
	tc, err := makeClient(cf, false)
	if err != nil {
		return trace.Wrap(err)
	}
	flags := scp.Flags{
		Recursive:     cf.RecursiveCopy,
		PreserveAttrs: cf.PreserveAttrs,
	}
	err = client.RetryWithRelogin(cf.Context, tc, func() error {
		return tc.SCP(cf.Context, cf.CopySpec, int(cf.NodePort), flags, cf.Quiet)
	})
	if err == nil {
		return nil
	}
	// exit with the same exit status as the failed command:
	if tc.ExitStatus != 0 {
		fmt.Fprintln(os.Stderr, utils.UserMessageFromError(err))
		os.Exit(tc.ExitStatus)
	}
	return trace.Wrap(err)
}

// makeClient takes the command-line configuration and constructs & returns
// a fully configured TeleportClient object
func makeClient(cf *CLIConf, useProfileLogin bool) (*client.TeleportClient, error) {
	// Parse OpenSSH style options.
	options, err := parseOptions(cf.Options)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	// apply defaults
	if cf.MinsToLive == 0 {
		cf.MinsToLive = int32(apidefaults.CertDuration / time.Minute)
	}

	// split login & host
	hostLogin := cf.NodeLogin
	var labels map[string]string
	if cf.UserHost != "" {
		parts := strings.Split(cf.UserHost, "@")
		partsLength := len(parts)
		if partsLength > 1 {
			hostLogin = strings.Join(parts[:partsLength-1], "@")
			cf.UserHost = parts[partsLength-1]
		}
		// see if remote host is specified as a set of labels
		if strings.Contains(cf.UserHost, "=") {
			labels, err = client.ParseLabelSpec(cf.UserHost)
			if err != nil {
				return nil, err
			}
		}
	} else if cf.CopySpec != nil {
		for _, location := range cf.CopySpec {
			// Extract username and host from "username@host:file/path"
			parts := strings.Split(location, ":")
			parts = strings.Split(parts[0], "@")
			partsLength := len(parts)
			if partsLength > 1 {
				hostLogin = strings.Join(parts[:partsLength-1], "@")
				cf.UserHost = parts[partsLength-1]
				break
			}
		}
	}
	fPorts, err := client.ParsePortForwardSpec(cf.LocalForwardPorts)
	if err != nil {
		return nil, err
	}

	dPorts, err := client.ParseDynamicPortForwardSpec(cf.DynamicForwardedPorts)
	if err != nil {
		return nil, err
	}

	// 1: start with the defaults
	c := client.MakeDefaultConfig()

	// ProxyJump is an alias of Proxy flag
	if cf.ProxyJump != "" {
		hosts, err := utils.ParseProxyJump(cf.ProxyJump)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		c.JumpHosts = hosts
	}

	// Look if a user identity was given via -i flag
	if cf.IdentityFileIn != "" {
		// Ignore local authentication methods when identity file is provided
		c.SkipLocalAuth = true
		var (
			key          *client.Key
			identityAuth ssh.AuthMethod
			expiryDate   time.Time
			hostAuthFunc ssh.HostKeyCallback
		)
		// read the ID file and create an "auth method" from it:
		key, err = client.KeyFromIdentityFile(cf.IdentityFileIn)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		hostAuthFunc, err := key.HostKeyCallback(cf.InsecureSkipVerify)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		if hostAuthFunc != nil {
			c.HostKeyCallback = hostAuthFunc
		} else {
			return nil, trace.BadParameter("missing trusted certificate authorities in the identity, upgrade to newer version of tctl, export identity and try again")
		}
		certUsername, err := key.CertUsername()
		if err != nil {
			return nil, trace.Wrap(err)
		}
		log.Debugf("Extracted username %q from the identity file %v.", certUsername, cf.IdentityFileIn)
		c.Username = certUsername

		identityAuth, err = authFromIdentity(key)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		c.AuthMethods = []ssh.AuthMethod{identityAuth}

		// Also create an in-memory agent to hold the key. If cluster is in
		// proxy recording mode, agent forwarding will be required for
		// sessions.
		c.Agent = agent.NewKeyring()
		agentKeys, err := key.AsAgentKeys()
		if err != nil {
			return nil, trace.Wrap(err)
		}
		for _, k := range agentKeys {
			if err := c.Agent.Add(k); err != nil {
				return nil, trace.Wrap(err)
			}
		}

		if len(key.TLSCert) > 0 {
			c.TLS, err = key.TeleportClientTLSConfig(nil)
			if err != nil {
				return nil, trace.Wrap(err)
			}
		}
		// check the expiration date
		expiryDate, _ = key.CertValidBefore()
		if expiryDate.Before(time.Now()) {
			fmt.Fprintf(os.Stderr, "WARNING: the certificate has expired on %v\n", expiryDate)
		}
	} else {
		// load profile. if no --proxy is given the currently active profile is used, otherwise
		// fetch profile for exact proxy we are trying to connect to.
		err = c.LoadProfile(cf.HomePath, cf.Proxy)
		if err != nil {
			fmt.Printf("WARNING: Failed to load tsh profile for %q: %v\n", cf.Proxy, err)
		}
	}
	// 3: override with the CLI flags
	if cf.Namespace != "" {
		c.Namespace = cf.Namespace
	}
	if cf.Username != "" {
		c.Username = cf.Username
	}
	// if proxy is set, and proxy is not equal to profile's
	// loaded addresses, override the values
	if err := setClientWebProxyAddr(cf, c); err != nil {
		return nil, trace.Wrap(err)
	}

	if len(fPorts) > 0 {
		c.LocalForwardPorts = fPorts
	}
	if len(dPorts) > 0 {
		c.DynamicForwardedPorts = dPorts
	}
	profileSiteName := c.SiteName
	if cf.SiteName != "" {
		c.SiteName = cf.SiteName
	}
	if cf.KubernetesCluster != "" {
		c.KubernetesCluster = cf.KubernetesCluster
	}
	if cf.DatabaseService != "" {
		c.DatabaseService = cf.DatabaseService
	}
	// if host logins stored in profiles must be ignored...
	if !useProfileLogin {
		c.HostLogin = ""
	}
	if hostLogin != "" {
		c.HostLogin = hostLogin
	}
	c.Host = cf.UserHost
	c.HostPort = int(cf.NodePort)
	c.Labels = labels
	c.KeyTTL = time.Minute * time.Duration(cf.MinsToLive)
	c.InsecureSkipVerify = cf.InsecureSkipVerify

	// If a TTY was requested, make sure to allocate it. Note this applies to
	// "exec" command because a shell always has a TTY allocated.
	if cf.Interactive || options.RequestTTY {
		c.Interactive = true
	}

	if !cf.NoCache {
		c.CachePolicy = &client.CachePolicy{}
	}

	// check version compatibility of the server and client
	c.CheckVersions = !cf.SkipVersionCheck

	// parse compatibility parameter
	certificateFormat, err := parseCertificateCompatibilityFlag(cf.Compatibility, cf.CertificateFormat)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	c.CertificateFormat = certificateFormat

	// copy the authentication connector over
	if cf.AuthConnector != "" {
		c.AuthConnector = cf.AuthConnector
	}

	// If agent forwarding was specified on the command line enable it.
	c.ForwardAgent = options.ForwardAgent
	if cf.ForwardAgent {
		c.ForwardAgent = client.ForwardAgentYes
	}

	// If X11 trusted/untrusted forwarding was specified on the command line enable it.
	c.X11Forwarding = cf.X11Forwarding
	c.X11ForwardingTrusted = cf.X11ForwardingTrusted

	// If the caller does not want to check host keys, pass in a insecure host
	// key checker.
	if !options.StrictHostKeyChecking {
		c.HostKeyCallback = client.InsecureSkipHostKeyChecking
	}
	c.BindAddr = cf.BindAddr

	// Don't execute remote command, used when port forwarding.
	c.NoRemoteExec = cf.NoRemoteExec

	// Allow the default browser used to open tsh login links to be overridden
	// (not currently implemented) or set to 'none' to suppress browser opening entirely.
	c.Browser = cf.Browser

	c.AddKeysToAgent = cf.AddKeysToAgent
	if !cf.UseLocalSSHAgent {
		c.AddKeysToAgent = client.AddKeysToAgentNo
	}

	c.EnableEscapeSequences = cf.EnableEscapeSequences

	// pass along mock sso login if provided (only used in tests)
	c.MockSSOLogin = cf.mockSSOLogin

	// Set tsh home directory
	c.HomePath = cf.HomePath

	if c.KeysDir == "" {
		c.KeysDir = c.HomePath
	}

	tc, err := client.NewClient(c)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	// Load SSH key for the cluster indicated in the profile.
	// Handle gracefully if the profile is empty or if the key cannot be found.
	if profileSiteName != "" {
		if err := tc.LoadKeyForCluster(profileSiteName); err != nil {
			log.Debug(err)
			if !trace.IsNotFound(err) {
				return nil, trace.Wrap(err)
			}
		}
	}

	// If identity file was provided, we skip loading the local profile info
	// (above). This profile info provides the proxy-advertised listening
	// addresses.
	// To compensate, when using an identity file, explicitly fetch these
	// addresses from the proxy (this is what Ping does).
	if cf.IdentityFileIn != "" {
		log.Debug("Pinging the proxy to fetch listening addresses for non-web ports.")
		if _, err := tc.Ping(cf.Context); err != nil {
			return nil, trace.Wrap(err)
		}
	}

	return tc, nil
}

// defaultWebProxyPorts is the order of default proxy ports to try, in order that
// they will be tried.
var defaultWebProxyPorts = []int{
	defaults.HTTPListenPort, teleport.StandardHTTPSPort,
}

// setClientWebProxyAddr configures the client WebProxyAddr and SSHProxyAddr
// configuration values. Values that are not fully specified via configuration
// or command-line options will be deduced if necessary.
//
// If successful, setClientWebProxyAddr will modify the client Config in-place.
func setClientWebProxyAddr(cf *CLIConf, c *client.Config) error {
	// If the user has specified a proxy on the command line, and one has not
	// already been specified from configuration...

	if cf.Proxy != "" && c.WebProxyAddr == "" {
		parsedAddrs, err := client.ParseProxyHost(cf.Proxy)
		if err != nil {
			return trace.Wrap(err)
		}

		proxyAddress := parsedAddrs.WebProxyAddr
		if parsedAddrs.UsingDefaultWebProxyPort {
			log.Debug("Web proxy port was not set. Attempting to detect port number to use.")
			timeout, cancel := context.WithTimeout(context.Background(), proxyDefaultResolutionTimeout)
			defer cancel()

			proxyAddress, err = pickDefaultAddr(
				timeout, cf.InsecureSkipVerify, parsedAddrs.Host, defaultWebProxyPorts)

			// On error, fall back to the legacy behaviour
			if err != nil {
				log.WithError(err).Debug("Proxy port resolution failed, falling back to legacy default.")
				return c.ParseProxyHost(cf.Proxy)
			}
		}

		c.WebProxyAddr = proxyAddress
		c.SSHProxyAddr = parsedAddrs.SSHProxyAddr
	}

	return nil
}

func parseCertificateCompatibilityFlag(compatibility string, certificateFormat string) (string, error) {
	switch {
	// if nothing is passed in, the role will decide
	case compatibility == "" && certificateFormat == "":
		return teleport.CertificateFormatUnspecified, nil
	// supporting the old --compat format for backward compatibility
	case compatibility != "" && certificateFormat == "":
		return utils.CheckCertificateFormatFlag(compatibility)
	// new documented flag --cert-format
	case compatibility == "" && certificateFormat != "":
		return utils.CheckCertificateFormatFlag(certificateFormat)
	// can not use both
	default:
		return "", trace.BadParameter("--compat or --cert-format must be specified")
	}
}

// refuseArgs helper makes sure that 'args' (list of CLI arguments)
// does not contain anything other than command
func refuseArgs(command string, args []string) error {
	for _, arg := range args {
		if arg == command || strings.HasPrefix(arg, "-") {
			continue
		} else {
			return trace.BadParameter("unexpected argument: %s", arg)
		}

	}
	return nil
}

// authFromIdentity returns a standard ssh.Authmethod for a given identity file
func authFromIdentity(k *client.Key) (ssh.AuthMethod, error) {
	signer, err := sshutils.NewSigner(k.Priv, k.Cert)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return ssh.PublicKeys(signer), nil
}

// onShow reads an identity file (a public SSH key or a cert) and dumps it to stdout
func onShow(cf *CLIConf) error {
	key, err := client.KeyFromIdentityFile(cf.IdentityFileIn)
	if err != nil {
		return trace.Wrap(err)
	}

	// unmarshal certificate bytes into a ssh.PublicKey
	cert, _, _, _, err := ssh.ParseAuthorizedKey(key.Cert)
	if err != nil {
		return trace.Wrap(err)
	}

	// unmarshal private key bytes into a *rsa.PrivateKey
	priv, err := ssh.ParseRawPrivateKey(key.Priv)
	if err != nil {
		return trace.Wrap(err)
	}

	pub, err := ssh.ParsePublicKey(key.Pub)
	if err != nil {
		return trace.Wrap(err)
	}

	fmt.Printf("Cert: %#v\nPriv: %#v\nPub: %#v\n",
		cert, priv, pub)

	fmt.Printf("Fingerprint: %s\n", ssh.FingerprintSHA256(pub))
	return nil
}

// printStatus prints the status of the profile.
func printStatus(debug bool, p *client.ProfileStatus, isActive bool) {
	var count int
	var prefix string
	if isActive {
		prefix = "> "
	} else {
		prefix = "  "
	}
	duration := time.Until(p.ValidUntil)
	humanDuration := "EXPIRED"
	if duration.Nanoseconds() > 0 {
		humanDuration = fmt.Sprintf("valid for %v", duration.Round(time.Minute))
	}

	fmt.Printf("%vProfile URL:        %v\n", prefix, p.ProxyURL.String())
	fmt.Printf("  Logged in as:       %v\n", p.Username)
	if p.Cluster != "" {
		fmt.Printf("  Cluster:            %v\n", p.Cluster)
	}
	fmt.Printf("  Roles:              %v\n", strings.Join(p.Roles, ", "))
	if debug {
		for k, v := range p.Traits {
			if count == 0 {
				fmt.Printf("  Traits:             %v: %v\n", k, v)
			} else {
				fmt.Printf("                      %v: %v\n", k, v)
			}
			count = count + 1
		}
	}
	fmt.Printf("  Logins:             %v\n", strings.Join(p.Logins, ", "))
	if p.KubeEnabled {
		fmt.Printf("  Kubernetes:         enabled\n")
		if kubeCluster := selectedKubeCluster(p.Cluster); kubeCluster != "" {
			fmt.Printf("  Kubernetes cluster: %q\n", kubeCluster)
		}
		if len(p.KubeUsers) > 0 {
			fmt.Printf("  Kubernetes users:   %v\n", strings.Join(p.KubeUsers, ", "))
		}
		if len(p.KubeGroups) > 0 {
			fmt.Printf("  Kubernetes groups:  %v\n", strings.Join(p.KubeGroups, ", "))
		}
	} else {
		fmt.Printf("  Kubernetes:         disabled\n")
	}
	if len(p.Databases) != 0 {
		fmt.Printf("  Databases:          %v\n", strings.Join(p.DatabaseServices(), ", "))
	}
	fmt.Printf("  Valid until:        %v [%v]\n", p.ValidUntil, humanDuration)
	fmt.Printf("  Extensions:         %v\n", strings.Join(p.Extensions, ", "))

	fmt.Printf("\n")
}

// onStatus command shows which proxy the user is logged into and metadata
// about the certificate.
func onStatus(cf *CLIConf) error {
	// Get the status of the active profile as well as the status
	// of any other proxies the user is logged into.
	//
	// Return error if not logged in, no active profile, or expired.
	profile, profiles, err := client.Status(cf.HomePath, cf.Proxy)
	if err != nil {
		return trace.Wrap(err)
	}

	printProfiles(cf.Debug, profile, profiles)

	if profile == nil {
		return trace.NotFound("Not logged in.")
	}

	duration := time.Until(profile.ValidUntil)
	if !profile.ValidUntil.IsZero() && duration.Nanoseconds() <= 0 {
		return trace.NotFound("Active profile expired.")
	}

	return nil
}

func printProfiles(debug bool, profile *client.ProfileStatus, profiles []*client.ProfileStatus) {
	if profile == nil && len(profiles) == 0 {
		return
	}

	// Print the active profile.
	if profile != nil {
		printStatus(debug, profile, true)
	}

	// Print all other profiles.
	for _, p := range profiles {
		printStatus(debug, p, false)
	}
}

// host is a utility function that extracts
// host from the host:port pair, in case of any error
// returns the original value
func host(in string) string {
	out, err := utils.Host(in)
	if err != nil {
		return in
	}
	return out
}

// waitForRequestResolution waits for an access request to be resolved.
func waitForRequestResolution(cf *CLIConf, tc *client.TeleportClient, req types.AccessRequest) error {
	filter := types.AccessRequestFilter{
		User: req.GetUser(),
	}
	var err error
	var watcher types.Watcher
	err = tc.WithRootClusterClient(cf.Context, func(clt auth.ClientI) error {
		watcher, err = tc.NewWatcher(cf.Context, types.Watch{
			Name: "await-request-approval",
			Kinds: []types.WatchKind{{
				Kind:   types.KindAccessRequest,
				Filter: filter.IntoMap(),
			}},
		})
		return trace.Wrap(err)
	})

	if err != nil {
		return trace.Wrap(err)
	}
	defer watcher.Close()
Loop:
	for {
		select {
		case event := <-watcher.Events():
			switch event.Type {
			case types.OpInit:
				log.Infof("Access-request watcher initialized...")
				continue Loop
			case types.OpPut:
				r, ok := event.Resource.(*types.AccessRequestV3)
				if !ok {
					return trace.BadParameter("unexpected resource type %T", event.Resource)
				}
				if r.GetName() != req.GetName() || r.GetState().IsPending() {
					log.Debugf("Skipping put event id=%s,state=%s.", r.GetName(), r.GetState())
					continue Loop
				}
				return onRequestResolution(cf, tc, r)
			case types.OpDelete:
				if event.Resource.GetName() != req.GetName() {
					log.Debugf("Skipping delete event id=%s", event.Resource.GetName())
					continue Loop
				}
				return trace.Errorf("request %s has expired or been deleted...", event.Resource.GetName())
			default:
				log.Warnf("Skipping unknown event type %s", event.Type)
			}
		case <-watcher.Done():
			return trace.Wrap(watcher.Error())
		}
	}
}

func onRequestResolution(cf *CLIConf, tc *client.TeleportClient, req types.AccessRequest) error {
	if !req.GetState().IsApproved() {
		msg := fmt.Sprintf("request %s has been set to %s", req.GetName(), req.GetState().String())
		if reason := req.GetResolveReason(); reason != "" {
			msg = fmt.Sprintf("%s, reason=%q", msg, reason)
		}
		return trace.Errorf(msg)
	}

	msg := "\nApproval received, getting updated certificates...\n\n"
	if reason := req.GetResolveReason(); reason != "" {
		msg = fmt.Sprintf("\nApproval received, reason=%q\nGetting updated certificates...\n\n", reason)
	}
	fmt.Fprint(os.Stderr, msg)

	err := reissueWithRequests(cf, tc, req.GetName())
	return trace.Wrap(err)
}

// reissueWithRequests handles a certificate reissue, applying new requests by ID,
// and saving the updated profile.
func reissueWithRequests(cf *CLIConf, tc *client.TeleportClient, reqIDs ...string) error {
	profile, err := client.StatusCurrent(cf.HomePath, cf.Proxy)
	if err != nil {
		return trace.Wrap(err)
	}
	params := client.ReissueParams{
		AccessRequests: reqIDs,
		RouteToCluster: cf.SiteName,
	}
	// if the certificate already had active requests, add them to our inputs parameters.
	if len(profile.ActiveRequests.AccessRequests) > 0 {
		params.AccessRequests = append(params.AccessRequests, profile.ActiveRequests.AccessRequests...)
	}
	if params.RouteToCluster == "" {
		params.RouteToCluster = profile.Cluster
	}
	if err := tc.ReissueUserCerts(cf.Context, client.CertCacheDrop, params); err != nil {
		return trace.Wrap(err)
	}
	if err := tc.SaveProfile("", true); err != nil {
		return trace.Wrap(err)
	}
	if err := updateKubeConfig(cf, tc, ""); err != nil {
		return trace.Wrap(err)
	}
	return nil
}

func onApps(cf *CLIConf) error {
	tc, err := makeClient(cf, false)
	if err != nil {
		return trace.Wrap(err)
	}

	// Get a list of all applications.
	var apps []types.Application
	err = client.RetryWithRelogin(cf.Context, tc, func() error {
		apps, err = tc.ListApps(cf.Context)
		return err
	})
	if err != nil {
		return trace.Wrap(err)
	}

	// Retrieve profile to be able to show which apps user is logged into.
	profile, err := client.StatusCurrent(cf.HomePath, cf.Proxy)
	if err != nil {
		return trace.Wrap(err)
	}

	// Sort by app name.
	sort.Slice(apps, func(i, j int) bool {
		return apps[i].GetName() < apps[j].GetName()
	})

	showApps(apps, profile.Apps, cf.Verbose)
	return nil
}

// onEnvironment handles "tsh env" command.
func onEnvironment(cf *CLIConf) error {
	profile, err := client.StatusCurrent(cf.HomePath, cf.Proxy)
	if err != nil {
		return trace.Wrap(err)
	}

	// Print shell built-in commands to set (or unset) environment.
	switch {
	case cf.unsetEnvironment:
		fmt.Printf("unset %v\n", proxyEnvVar)
		fmt.Printf("unset %v\n", clusterEnvVar)
		fmt.Printf("unset %v\n", kubeClusterEnvVar)
		fmt.Printf("unset %v\n", teleport.EnvKubeConfig)
	case !cf.unsetEnvironment:
		fmt.Printf("export %v=%v\n", proxyEnvVar, profile.ProxyURL.Host)
		fmt.Printf("export %v=%v\n", clusterEnvVar, profile.Cluster)
		if kubeName := selectedKubeCluster(profile.Cluster); kubeName != "" {
			fmt.Printf("export %v=%v\n", kubeClusterEnvVar, kubeName)
			fmt.Printf("# set %v to a standalone kubeconfig for the selected kube cluster\n", teleport.EnvKubeConfig)
			fmt.Printf("export %v=%v\n", teleport.EnvKubeConfig, profile.KubeConfigPath(kubeName))
		}
	}

	return nil
}

// envGetter is used to read in the environment. In production "os.Getenv"
// is used.
type envGetter func(string) string

// setEnvFlags sets flags that can be set via environment variables.
func setEnvFlags(cf *CLIConf, fn envGetter) {
	// prioritize CLI input
	if cf.SiteName == "" {
		setSiteNameFromEnv(cf, fn)
	}
	// prioritize CLI input
	if cf.KubernetesCluster == "" {
		setKubernetesClusterFromEnv(cf, fn)
	}
	setTeleportHomeFromEnv(cf, fn)
}

// setSiteNameFromEnv sets teleport site name from environment if configured.
// First try reading TELEPORT_CLUSTER, then the legacy term TELEPORT_SITE.
func setSiteNameFromEnv(cf *CLIConf, fn envGetter) {
	if clusterName := fn(siteEnvVar); clusterName != "" {
		cf.SiteName = clusterName
	}
	if clusterName := fn(clusterEnvVar); clusterName != "" {
		cf.SiteName = clusterName
	}
}

// setTeleportHomeFromEnv sets home directory from environment if configured.
func setTeleportHomeFromEnv(cf *CLIConf, fn envGetter) {
	if homeDir := fn(homeEnvVar); homeDir != "" {
		cf.HomePath = path.Clean(homeDir)
	}
}

// setKubernetesClusterFromEnv sets teleport kube cluster from environment if configured.
func setKubernetesClusterFromEnv(cf *CLIConf, fn envGetter) {
	if kubeName := fn(kubeClusterEnvVar); kubeName != "" {
		cf.KubernetesCluster = kubeName
	}
}

func handleUnimplementedError(ctx context.Context, perr error, cf CLIConf) error {
	const (
		errMsgFormat         = "This server does not implement this feature yet. Likely the client version you are using is newer than the server. The server version: %v, the client version: %v. Please upgrade the server."
		unknownServerVersion = "unknown"
	)
	tc, err := makeClient(&cf, false)
	if err != nil {
		log.WithError(err).Warning("Failed to create client.")
		return trace.WrapWithMessage(perr, errMsgFormat, unknownServerVersion, teleport.Version)
	}
	pr, err := tc.Ping(ctx)
	if err != nil {
		log.WithError(err).Warning("Failed to call ping.")
		return trace.WrapWithMessage(perr, errMsgFormat, unknownServerVersion, teleport.Version)
	}
	return trace.WrapWithMessage(perr, errMsgFormat, pr.ServerVersion, teleport.Version)
}
