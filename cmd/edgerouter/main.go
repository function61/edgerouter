// Main entrypoint for all things Edgerouter
package main

import (
	"log/slog"

	"github.com/function61/edgerouter/pkg/erbackend/turbochargerbackend/turbochargererdeploy"
	"github.com/function61/edgerouter/pkg/erlambdacli"
	"github.com/function61/edgerouter/pkg/ers3cli"
	"github.com/function61/edgerouter/pkg/erserver"
	"github.com/function61/edgerouter/pkg/turbocharger/turbochargerdeploy"
	"github.com/function61/eventhorizon/pkg/ehcli"
	"github.com/function61/gokit/app/cli"
	"github.com/spf13/cobra"
)

func main() {
	app := &cobra.Command{
		Short: "Lean and mean edge router from function61.com",
	}

	app.AddCommand(discoveryEntry())
	app.AddCommand(serveEntry())
	app.AddCommand(turbochargerEntrypoint())

	app.AddCommand(ers3cli.Entrypoint())
	app.AddCommand(erlambdacli.Entrypoint())

	// Event Horizon administration
	app.AddCommand(ehcli.Entrypoint())

	cli.Execute(app)
}

func serveEntry() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Runs the HTTP server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return erserver.Serve(cmd.Context(), erserver.DefaultConfigDir, slog.Default())
		},
	}
}

func turbochargerEntrypoint() *cobra.Command {
	turbochargerCmd := turbochargerdeploy.CLIEntrypoint()
	turbochargerCmd.AddCommand(turbochargererdeploy.CLIEntrypoint())
	return turbochargerCmd
}
