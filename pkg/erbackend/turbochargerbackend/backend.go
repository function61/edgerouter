// An entire static website directly hosted in Turbocharger.
// There also exists Turbocharger in Lambda etc. backends where we support turbocharging just the sub-trees of apps.
package turbochargerbackend

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/function61/edgerouter/pkg/turbocharger"
)

// doesn't do much more than binds a static manifest ID to the backend. in case the manifest changes
// (a different version of a website gets deployed), that's an Edgerouter-level concern and it will make a new backend instance.
func New(ctx context.Context, manifestID turbocharger.ObjectID, logger *slog.Logger) (http.Handler, error) {
	manifestHandler, err := turbocharger.GetManifestHandlerSingleton(ctx, logger)
	if err != nil {
		return nil, err
	}

	backendLogger := logger.With("subsystem", "turbocharger-backend")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") { // "/foo/" => "/foo/index.html"
			r.URL.Path += "index.html"
		}

		if err := manifestHandler.ServeHTTPFromManifest(manifestID, w, r); err != nil {
			backendLogger.Error("serve from manifest",
				"error", err,
				"manifest_id", manifestID.String(),
				"path", r.URL.Path,
			)
		}
	}), nil
}
