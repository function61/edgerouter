// Application discovery - finding backends and frotends to route traffic to
package erdiscovery

import (
	"context"
	"github.com/function61/edgerouter/pkg/erconfig"
)

type Reader interface {
	ReadApplications(context.Context) ([]erconfig.Application, error)
}

type Writer interface {
	UpdateApplication(context.Context, erconfig.Application) error
	DeleteApplication(context.Context, erconfig.Application) error
}

type ReaderWriter interface {
	Reader
	Writer
}
