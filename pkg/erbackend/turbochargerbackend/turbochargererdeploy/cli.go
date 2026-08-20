// Deploys Turbocharger static site to Edgerouter
package turbochargererdeploy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery/defaultdiscovery"
	"github.com/function61/edgerouter/pkg/turbocharger"
	"github.com/spf13/cobra"
)

func CLIEntrypoint() *cobra.Command {
	return &cobra.Command{
		Use:   "deploy-site-from-store [applicationId] [manifestID]",
		Short: "Deploys a static website from Turbocharger",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := slog.Default()

			manifestID, err := turbocharger.ObjectIDFromString(args[1])
			if err != nil {
				return err
			}

			return deploy(cmd.Context(), args[0], *manifestID, logger)
		},
	}
}

// atomically deploys a new version of a site by changing site's Turbocharger Manifest ID
// (which is essentially a pointer to an immutable file list) in the app configuration.
func deploy(ctx context.Context, applicationID string, manifestID turbocharger.ObjectID, logger *slog.Logger) error {
	discoverySvc, err := defaultdiscovery.New(logger)
	if err != nil {
		return err
	}

	apps, err := discoverySvc.ReadApplications(ctx)
	if err != nil {
		return err
	}

	app := erconfig.FindApplication(applicationID, apps)
	if app == nil {
		return fmt.Errorf("unknown applicationId: %s", applicationID)
	}

	if app.Backend.Kind != erconfig.BackendKindTurbocharger {
		return fmt.Errorf(
			"invalid app type; expecting %s, got %s",
			erconfig.BackendKindTurbocharger,
			app.Backend.Kind)
	}

	// just point to a new version
	app.Backend = erconfig.TurbochargerBackend(manifestID)

	return discoverySvc.UpdateApplication(ctx, *app)
}
