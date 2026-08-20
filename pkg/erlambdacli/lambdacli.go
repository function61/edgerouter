// CLI for managing Lambda application backends
package erlambdacli

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery/defaultdiscovery"
	"github.com/spf13/cobra"
)

func Entrypoint() *cobra.Command {
	app := &cobra.Command{
		Use:   "lambda",
		Short: "Lambda commands",
	}

	app.AddCommand(mkEntrypoint())

	return app
}

func mkEntrypoint() *cobra.Command {
	stripPath := false

	cmd := &cobra.Command{
		Use:   "mk [applicationId] [hostname] [path] [functionName] [regionId]",
		Short: "Create application definition for Lambda function",
		Args:  cobra.ExactArgs(5),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mk(cmd.Context(), args[0], args[1], args[2], stripPath, args[3], args[4])
		},
	}
	cmd.Flags().BoolVarP(&stripPath, "strip-path", "s", stripPath, "Strips path prefix before forwarding")
	return cmd
}

func mk(ctx context.Context, applicationID string, hostname string, path string, stripPath bool, functionName string, regionID string) error {
	discoverySvc, err := defaultdiscovery.New(slog.Default())
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	existingApplications, err := discoverySvc.ReadApplications(ctx)
	if err != nil {
		return err
	}

	duplicate := erconfig.FindApplication(applicationID, existingApplications)
	if duplicate != nil {
		return errors.New("application already exists")
	}

	opts := []erconfig.FrontendOpt{erconfig.PathPrefix(path)}
	if stripPath {
		opts = append(opts, erconfig.StripPathPrefix)
	}

	app := erconfig.SimpleApplication(
		applicationID,
		erconfig.SimpleHostnameFrontend(hostname, opts...),
		erconfig.LambdaBackend(functionName, regionID)) // version is empty for now - no deployment yet

	return discoverySvc.UpdateApplication(ctx, app)
}
