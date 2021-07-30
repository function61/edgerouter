package turbochargerdeploy

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/function61/edgerouter/pkg/turbocharger"
	"github.com/function61/gokit/logex"
	"github.com/function61/gokit/osutil"
	"github.com/spf13/cobra"
)

func CLIEntrypoint() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "turbocharger",
		Short: "Angry web-scale CAS",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "tar-deploy-to-store <project>",
		Short: "Deploy a tar package into the storage, so it can be referenced from somewhere",
		Args:  cobra.ExactArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			rootLogger := logex.StandardLogger()

			osutil.ExitIfError(tarDeploy(
				osutil.CancelOnInterruptOrTerminate(rootLogger),
				args[0],
				os.Stdin,
				rootLogger))
		},
	})

	return cmd
}

func tarDeploy(ctx context.Context, project string, tarStream io.Reader, logger *log.Logger) error {
	if project == "" {
		return errors.New("project cannot be empty")
	}

	storages, err := turbocharger.StorageFromConfig()
	if err != nil {
		return err
	}

	dm := turbocharger.NewDeploymentManager(*storages, logger)

	tarReader := tar.NewReader(tarStream)

	started := time.Now()

	manifest, err := dm.Deploy(ctx, turbocharger.NewMetadata(project), func() (*turbocharger.FileToDeploy, error) {
		for { // need loop to skip over directories
			tarFile, err := tarReader.Next()
			if err != nil {
				if err == io.EOF {
					return nil, nil // done
				} else {
					return nil, err
				}
			}

			if tarFile.FileInfo().IsDir() { // directories have no content we'd need to store
				continue
			}

			if strings.HasPrefix(tarFile.Name, ".") || strings.HasPrefix(tarFile.Name, "/") {
				return nil, fmt.Errorf("tar file not relative to root: %s", tarFile.Name)
			}

			return &turbocharger.FileToDeploy{Path: "/" + tarFile.Name, Content: tarReader}, nil
		}
	})
	if err != nil {
		return err
	}

	logex.Levels(logger).Info.Printf(
		"manifest %s and %d files have been deployed to CAS in %s\n",
		manifest.ID.String(),
		len(manifest.Manifest.Files),
		time.Since(started))

	return nil
}
