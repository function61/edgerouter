// "Meta" package - builds instance of the default discovery.
// Some parts of Edgerouter just assume one default discovery method, and this builds it.
package defaultdiscovery

import (
	"log"

	"github.com/function61/edgerouter/pkg/erdiscovery"
	"github.com/function61/edgerouter/pkg/erdiscovery/ehdiscovery"
	"github.com/function61/eventhorizon/pkg/ehreader"
	"github.com/function61/gokit/logex"
)

// currently uses ehdiscovery as default
func New(logger *log.Logger) (erdiscovery.ReaderWriter, error) {
	tenantCtx, err := ehreader.TenantCtxFrom(ehreader.ConfigFromEnv)
	if err != nil {
		return nil, err
	}

	return ehdiscovery.New(*tenantCtx, logex.Prefix("ehdiscovery", logger))
}
