package erdiscovery

import (
	"context"

	"github.com/function61/edgerouter/pkg/erconfig"
)

type staticDiscovery struct {
	apps []erconfig.Application
}

func StaticDiscovery(apps []erconfig.Application) Reader {
	return &staticDiscovery{apps}
}

func (s *staticDiscovery) ReadApplications(ctx context.Context) ([]erconfig.Application, error) {
	return s.apps, nil
}
