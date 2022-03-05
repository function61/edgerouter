// Discover applications from a JSON "config" file
package filediscovery

import (
	"context"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery"
	"github.com/function61/gokit/jsonfile"
)

const (
	DefaultFilename = "applications.json"
)

// file format for discovery file.
// NOTE: not having applications at top level for extensibility + prepare for `$schema`
type file struct {
	Apps []erconfig.Application `json:"apps"`
}

type fileDiscovery struct {
	file string
}

var _ erdiscovery.Reader = (*fileDiscovery)(nil)

func New(file string) erdiscovery.Reader {
	return &fileDiscovery{file}
}

func (f *fileDiscovery) ReadApplications(_ context.Context) ([]erconfig.Application, error) {
	appsFromFile := file{}
	if err := jsonfile.Read(f.file, &appsFromFile, true); err != nil {
		return nil, err
	}
	return appsFromFile.Apps, nil
}
