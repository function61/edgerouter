// Code required to bring turbocharger support to concrete applications
package turbochargerapp

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/function61/gokit/osutil"
	"github.com/spf13/cobra"
)

const (
	turbochargerAdvertisementHeaderKey = "turbocharger"
)

// if deployment specifies TURBOCHARGER_MANIFEST, all requests to a prefix (e.g. /static/...)
// get "turbocharger" HTTP header added, which instructs any loadbalancer that all /static/... can
// be henceforth served from a CAS with an approach that has very aggressive caching capabilities
func WrapWithAdvertisement(prefix string, backing http.Handler) http.Handler {
	manifestID := os.Getenv("TURBOCHARGER_MANIFEST")
	if manifestID == "" { // ENV not defined. skip turbocharger
		return backing
	}

	tcHeader := fmt.Sprintf("%s %s", prefix, manifestID)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the whole sub-tree gets an advertisement for turbocharger.
		// looks like "/static U4P2kMtkWVNyfZaLCDBXLJ4NptZowSmcJOMxHcZA72c"
		w.Header().Set(turbochargerAdvertisementHeaderKey, tcHeader)

		backing.ServeHTTP(w, r)
	})
}

// helper for making a static file handler with Turbocharger advertising enabled
func FileHandler(prefix string, files fs.FS) http.Handler {
	staticFilesServer := http.FileServer(http.FS(files))

	// adds "turbocharger: ..." header to all responses from staticFilesServer
	staticFilesServerTurbochargeAdvertised := WrapWithAdvertisement(prefix, staticFilesServer)

	// need to strip prefix because the filesystem only understands its local context
	return http.StripPrefix(prefix, staticFilesServerTurbochargeAdvertised)
}

func StaticFilesExportEntrypoint(files fs.ReadDirFS) *cobra.Command {
	return &cobra.Command{
		Use:   "static-files-export",
		Short: "Export static files as .tar (which can be deployed to turbocharger)",
		Run: func(*cobra.Command, []string) {
			osutil.ExitIfError(staticFilesExport(files, os.Stdout))
		},
	}
}

func staticFilesExport(files fs.ReadDirFS, output io.Writer) error {
	tarWriter := tar.NewWriter(output)

	var writeFilesFromOneDirectory func(string) error
	writeFilesFromOneDirectory = func(dir string) error {
		// 1st call needs to be ReadDir(".") but subdir needs to be ReadDir("subdir")
		entries, err := files.ReadDir(func() string {
			if dir == "" {
				return "."
			} else {
				return dir
			}
		}())
		if err != nil {
			return err
		}

		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				return err
			}

			fullPath := filepath.Join(dir, entry.Name())

			tarHeader, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			// *info* only contained base name, need to add dir prefix in front
			tarHeader.Name = func() string {
				if entry.IsDir() {
					return fullPath + "/"
				} else {
					return fullPath
				}
			}()

			if err := tarWriter.WriteHeader(tarHeader); err != nil {
				return err
			}

			if entry.IsDir() {
				// directories don't have content, but probably have children
				if err := writeFilesFromOneDirectory(fullPath); err != nil {
					return err
				}
			} else {
				if err := func() error {
					file, err := files.Open(fullPath)
					if err != nil {
						return err
					}
					defer file.Close()

					_, err = io.Copy(tarWriter, file)
					return err
				}(); err != nil {
					return err
				}
			}
		}

		return nil
	}

	if err := writeFilesFromOneDirectory(""); err != nil {
		return err
	}

	return tarWriter.Close()
}
