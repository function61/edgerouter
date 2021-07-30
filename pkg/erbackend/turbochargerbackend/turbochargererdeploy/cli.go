// Deploys Turbocharger static site to Edgerouter
package turbochargererdeploy

import (
	"context"
	"fmt"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery/defaultdiscovery"
	"github.com/function61/edgerouter/pkg/turbocharger"
	"github.com/function61/gokit/logex"
	"github.com/function61/gokit/osutil"
	"github.com/spf13/cobra"
)

func CLIEntrypoint() *cobra.Command {
	return &cobra.Command{
		Use:   "deploy-site-from-store [applicationId] [manifestID]",
		Short: "Deploys a static website from Turbocharger",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			osutil.ExitIfError(func() error {
				manifestID, err := turbocharger.ObjectIDFromString(args[1])
				if err != nil {
					return err
				}

				return deploy(
					osutil.CancelOnInterruptOrTerminate(logex.StandardLogger()),
					args[0],
					*manifestID)
			}())
		},
	}
}

// atomically deploys a new version of a site by changing site's Turbocharger Manifest ID
// (which is essentially a pointer to an immutable file list) in the app configuration.
func deploy(
	ctx context.Context,
	applicationId string,
	manifestID turbocharger.ObjectID,
) error {
	discoverySvc, err := defaultdiscovery.New(nil)
	if err != nil {
		return err
	}

	apps, err := discoverySvc.ReadApplications(ctx)
	if err != nil {
		return err
	}

	app := erconfig.FindApplication(applicationId, apps)
	if app == nil {
		return fmt.Errorf("unknown applicationId: %s", applicationId)
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
