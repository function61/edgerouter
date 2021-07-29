// An entire static website directly hosted in Turbocharger.
// There also exists Turbocharger in Lambda etc. backends where we support turbocharging just the sub-trees of apps.
package turbochargerbackend

import (
	"log"
	"net/http"
	"strings"

	"github.com/function61/edgerouter/pkg/turbocharger"
	"github.com/function61/gokit/logex"
)

// doesn't do much more than binds a static manifest ID to the backend. in case the manifest changes
// (a different version of a website gets deployed), that's an Edgerouter-level concern and it will make a new backend instance.
func New(manifestID turbocharger.ObjectID, logger *log.Logger) (http.Handler, error) {
	manifestHandler, err := turbocharger.GetManifestHandlerSingleton()
	if err != nil {
		return nil, err
	}

	logl := logex.Levels(logger)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") { // "/foo/" => "/foo/index.html"
			r.URL.Path += "index.html"
		}

		if err := manifestHandler.ServeHTTPFromManifest(manifestID, w, r); err != nil {
			logl.Error.Println(err.Error())
		}
	}), nil
}
