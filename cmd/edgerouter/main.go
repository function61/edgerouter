// Main entrypoint for all things Edgerouter
package main

import (
	"context"
	"os"

	"github.com/function61/edgerouter/pkg/erlambdacli"
	"github.com/function61/edgerouter/pkg/ers3cli"
	"github.com/function61/edgerouter/pkg/erserver"
	"github.com/function61/edgerouter/pkg/insecureredirector"
	"github.com/function61/eventhorizon/pkg/ehcli"
	"github.com/function61/gokit/dynversion"
	"github.com/function61/gokit/logex"
	"github.com/function61/gokit/osutil"
	"github.com/function61/gokit/taskrunner"
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

			mainLogger := logex.Prefix("main", rootLogger)
			tasks := taskrunner.New(osutil.CancelOnInterruptOrTerminate(mainLogger), mainLogger)

			tasks.Start("insecureredirector", func(ctx context.Context) error {
				return insecureredirector.Serve(ctx, logex.Prefix("insecureredirector", rootLogger))
			})
			tasks.Start("server", func(ctx context.Context) error {
				return erserver.Serve(ctx, rootLogger)
			})
			tasks.Start("metrics", func(ctx context.Context) error {
				return erserver.MetricsServer(ctx, logex.Prefix("metrics", rootLogger))
			})

			osutil.ExitIfError(tasks.Wait())
		},
	}
}
