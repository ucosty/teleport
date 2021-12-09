GITREF=`git describe --long --tags` aw aw aw aw 

# $(VERSION_GO) will be written to version.go
VERSION_GO="/* DO NOT EDIT THIS FILE. IT IS GENERATED BY 'make version'*/\n\n\
package teleport\n\
const( Version = \"$(VERSION)\" )\n\
// Gitref variable is automatically set to the output of "git-describe" \n\
// during the build process\n\
var Gitref string\n"

# $(API_VERSION_GO) will be written to api/version.go
API_VERSION_GO="/* DO NOT EDIT THIS FILE. IT IS GENERATED BY 'make version'*/\n\n\
package api\n\
const( Version = \"$(VERSION)\" )\n\
// Gitref variable is automatically set to the output of "git-describe" \n\
// during the build process\n\
var Gitref string\n"

# $(GIT_GO) will be written to gitref.go
GITREF_GO="/* DO NOT EDIT THIS FILE. IT IS GENERATED BY 'make version' */ \n\n\
package teleport\n\
func init() { Gitref = \"$(GITREF)\"}  "

#
# setver updates version.go and gitref.go with VERSION and GITREF vars
#
.PHONY:setver
setver: helm-version
	@printf $(VERSION_GO) | gofmt > version.go
	@printf $(API_VERSION_GO) | gofmt > ./api/version.go
	@printf $(GITREF_GO) | gofmt > gitref.go

# helm-version automatically updates the versions of Helm charts to match the version set in the Makefile,
# so that chart versions are also kept in sync when the Teleport version is updated for a release.
# If the version contains '-dev' (as it does on the master branch, or for development builds) then we get the latest
# published major version number by parsing a sorted list of git tags instead, to make deploying the chart from master
# work as expected. Version numbers are quoted as a string because Helm otherwise treats dotted decimals as floats.
# The weird -i usage is to make the sed commands work the same on both Linux and Mac. Test on both platforms if you change it.
.PHONY:helm-version
helm-version:
	for CHART in teleport-cluster teleport-kube-agent; do \
		sed -i'.bak' -e "s_^version:\ .*_version: \"$${VERSION}\"_g" examples/chart/$${CHART}/Chart.yaml || exit 1; \
		sed -i'.bak' -e "s_^appVersion:\ .*_appVersion: \"$${VERSION}\"_g" examples/chart/$${CHART}/Chart.yaml || exit 1; \
		rm -f examples/chart/$${CHART}/Chart.yaml.bak; \
	done
