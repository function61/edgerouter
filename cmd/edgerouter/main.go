// Main entrypoint for all things Edgerouter
package main

import (
	"os"

	"github.com/function61/edgerouter/pkg/erbackend/turbochargerbackend/turbochargererdeploy"
	"github.com/function61/edgerouter/pkg/erlambdacli"
	"github.com/function61/edgerouter/pkg/ers3cli"
	"github.com/function61/edgerouter/pkg/erserver"
	"github.com/function61/edgerouter/pkg/turbocharger/turbochargerdeploy"
	"github.com/function61/eventhorizon/pkg/ehcli"
	"github.com/function61/gokit/dynversion"
	"github.com/function61/gokit/logex"
	"github.com/function61/gokit/osutil"
	"github.com/spf13/cobra"
)

func main() {
	app := &cobra.Command{
		Use:     os.Args[0],
		Short:   "Lean and mean edge router from function61.com",
		Version: dynversion.Version,
	}

	app.AddCommand(discoveryEntry())
	app.AddCommand(serveEntry())
	app.AddCommand(turbochargerEntrypoint())

	app.AddCommand(ers3cli.Entrypoint())
	app.AddCommand(erlambdacli.Entrypoint())

	// Event Horizon administration
	app.AddCommand(ehcli.Entrypoint())

	osutil.ExitIfError(app.Execute())
}

func serveEntry() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Runs the HTTP server",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			rootLogger := logex.StandardLogger()

			osutil.ExitIfError(erserver.Serve(
				osutil.CancelOnInterruptOrTerminate(rootLogger),
				rootLogger))
		},
	}
}

func turbochargerEntrypoint() *cobra.Command {
	turbochargerCmd := turbochargerdeploy.CLIEntrypoint()
	turbochargerCmd.AddCommand(turbochargererdeploy.CLIEntrypoint())
	return turbochargerCmd
}
