package erserver

import (
	"fmt"
	"github.com/function61/edgerouter/pkg/erbackend"
	"github.com/function61/edgerouter/pkg/erbackend/lambdabackend"
	"github.com/function61/edgerouter/pkg/erbackend/peersetbackend"
	"github.com/function61/edgerouter/pkg/erbackend/statics3websitebackend"
	"github.com/function61/edgerouter/pkg/erconfig"
)

// TODO: make "fem" parameteter unnecessary
func makeBackend(
	appId string,
	backendConf erconfig.Backend,
	fem *frontendMatchers,
) (erbackend.Backend, error) {
	switch backendConf.Kind {
	case erconfig.BackendKindS3StaticWebsite:
		return statics3websitebackend.New(appId, *backendConf.S3StaticWebsiteOpts), nil
	case erconfig.BackendKindPeerSet:
		return peersetbackend.New(*backendConf.PeerSetOpts), nil
	case erconfig.BackendKindAwsLambda:
		return lambdabackend.New(*backendConf.AwsLambdaOpts), nil
	case erconfig.BackendKindEdgerouterAdmin:
		return newAdminBackend(fem), nil
	default:
		return nil, fmt.Errorf("unsupported backend kind: %s", backendConf.Kind)
	}
}
