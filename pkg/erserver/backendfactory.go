package erserver

import (
	"fmt"
	"github.com/function61/edgerouter/pkg/erbackend/authv0backend"
	"github.com/function61/edgerouter/pkg/erbackend/lambdabackend"
	"github.com/function61/edgerouter/pkg/erbackend/peersetbackend"
	"github.com/function61/edgerouter/pkg/erbackend/statics3websitebackend"
	"github.com/function61/edgerouter/pkg/erconfig"
	"net/http"
)

// TODO: make "fem" parameteter unnecessary
func makeBackend(
	appId string,
	backendConf erconfig.Backend,
	fem *frontendMatchers,
) (http.Handler, error) {
	switch backendConf.Kind {
	case erconfig.BackendKindS3StaticWebsite:
		return statics3websitebackend.New(appId, *backendConf.S3StaticWebsiteOpts), nil
	case erconfig.BackendKindPeerSet:
		return peersetbackend.New(*backendConf.PeerSetOpts)
	case erconfig.BackendKindAwsLambda:
		return lambdabackend.New(*backendConf.AwsLambdaOpts)
	case erconfig.BackendKindEdgerouterAdmin:
		return newAdminBackend(fem), nil
	case erconfig.BackendKindAuthV0:
		authorizedBackend, err := makeBackend(
			appId,
			backendConf.AuthV0Opts.AuthorizedBackend,
			fem)
		if err != nil {
			return nil, fmt.Errorf("authorizedBackend: %w", err)
		}

		return authv0backend.New(*backendConf.AuthV0Opts, authorizedBackend), nil
	default:
		return nil, fmt.Errorf("unsupported backend kind: %s", backendConf.Kind)
	}
}
