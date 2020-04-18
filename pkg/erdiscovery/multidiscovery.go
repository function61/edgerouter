package erdiscovery

import (
	"context"

	"github.com/function61/edgerouter/pkg/erconfig"
)

// merges multiple discovery readers into one reader that returns them aggregated
func MultiDiscovery(merge []Reader) Reader {
	return &multiDiscovery{merge}
}

type multiDiscovery struct {
	readers []Reader
}

func (m *multiDiscovery) ReadApplications(ctx context.Context) ([]erconfig.Application, error) {
	merged := []erconfig.Application{}

	for _, reader := range m.readers {
		result, err := reader.ReadApplications(ctx)
		if err != nil {
			return nil, err
		}

		merged = append(merged, result...)
	}

	return merged, nil
}
