package main

import (
	"context"
	"fmt"
	"github.com/function61/edgerouter/pkg/erlambdacli"
	"github.com/function61/edgerouter/pkg/ers3cli"
	"github.com/function61/edgerouter/pkg/erserver"
	"github.com/function61/edgerouter/pkg/insecureredirector"
	"github.com/function61/eventhorizon/pkg/ehcli"
	"github.com/function61/gokit/dynversion"
	"github.com/function61/gokit/logex"
	"github.com/function61/gokit/ossignal"
	"github.com/function61/gokit/taskrunner"
	"github.com/spf13/cobra"
	"os"
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
	for _, cmd := range ehcli.Entrypoints() {
		app.AddCommand(cmd)
	}

	if err := app.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serveEntry() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Runs the HTTP server",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			rootLogger := logex.StandardLogger()

			mainLogger := logex.Prefix("main", rootLogger)
			tasks := taskrunner.New(ossignal.InterruptOrTerminateBackgroundCtx(mainLogger), mainLogger)

			tasks.Start("insecureredirector", func(ctx context.Context, taskName string) error {
				return insecureredirector.Serve(ctx, logex.Prefix(taskName, rootLogger))
			})
			tasks.Start("server", func(ctx context.Context, taskName string) error {
				return erserver.Serve(ctx, rootLogger)
			})
			tasks.Start("metrics", func(ctx context.Context, taskName string) error {
				return erserver.MetricsServer(ctx, logex.Prefix(taskName, rootLogger))
			})

			if err := tasks.Wait(); err != nil {
				panic(err)
			}
		},
	}
}
