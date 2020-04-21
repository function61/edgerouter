// CLI for managing static websites in S3
package ers3cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/function61/edgerouter/pkg/erbackend/statics3websitebackend"
	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery/defaultdiscovery"
	"github.com/spf13/cobra"
)

func Entrypoint() *cobra.Command {
	app := &cobra.Command{
		Use:   "s3",
		Short: "S3-staticwebsite commands",
	}

	app.AddCommand(&cobra.Command{
		Use:   "deploy [applicationId] [deployVersion] [pathToArchive]",
		Short: "Deploys a static website to all edgerouter servers",
		Args:  cobra.ExactArgs(3),
		Run: func(cmd *cobra.Command, args []string) {
			exitIfError(s3Deploy(args[0], args[1], args[2]))
		},
	})

	app.AddCommand(s3MkEntry())

	return app
}

func s3Deploy(applicationId string, deployVersion string, pathToArchive string) error {
	discoverySvc, err := defaultdiscovery.New(nil)
	if err != nil {
		return err
	}

	tarArchive, err := os.Open(pathToArchive)
	if err != nil {
		return err
	}
	defer tarArchive.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	if err := statics3websitebackend.Deploy(ctx, tarArchive, applicationId, deployVersion, discoverySvc); err != nil {
		return fmt.Errorf("statics3websitebackend deploy: %w", err)
	}

	return nil
}

func s3Mk(applicationId string, hostname string, path string, stripPath bool, bucketName string, regionId string) error {
	discoverySvc, err := defaultdiscovery.New(nil)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	existingApplications, err := discoverySvc.ReadApplications(ctx)
	if err != nil {
		return err
	}

	duplicate := erconfig.FindApplication(applicationId, existingApplications)
	if duplicate != nil {
		return errors.New("application already exists")
	}

	app := erconfig.SimpleApplication(
		applicationId,
		erconfig.SimpleHostnameFrontend(hostname, path, stripPath),
		erconfig.S3Backend(bucketName, regionId, "")) // version is empty for now - no deployment yet

	return discoverySvc.UpdateApplication(ctx, app)
}

func s3MkEntry() *cobra.Command {
	stripPath := false

	cmd := &cobra.Command{
		Use:   "mk [applicationId] [hostname] [path] [bucketName] [regionId]",
		Short: "Create static website definition",
		Args:  cobra.ExactArgs(5),
		Run: func(cmd *cobra.Command, args []string) {
			exitIfError(s3Mk(args[0], args[1], args[2], stripPath, args[3], args[4]))
		},
	}

	cmd.Flags().BoolVarP(&stripPath, "strip-path", "s", stripPath, "Strips path prefix before forwarding")

	return cmd
}

func exitIfError(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
