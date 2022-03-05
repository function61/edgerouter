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

type fileDiscovery struct {
	file string
}

var _ erdiscovery.Reader = (*fileDiscovery)(nil)

func New(file string) erdiscovery.Reader {
	return &fileDiscovery{file}
}

func (f *fileDiscovery) ReadApplications(_ context.Context) ([]erconfig.Application, error) {
	appsFromFile := []erconfig.Application{}
	return appsFromFile, jsonfile.Read(f.file, &appsFromFile, true)
}
