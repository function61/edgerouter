// Embeddable CLI for Edgerouter server library.
// Think calling "yourapp proxy ..." and all subcommands are routed to Edgerouter-provided subcommands.
package erservercli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/function61/edgerouter/pkg/erserver"
	"github.com/function61/gokit/app/dynversion"
	"github.com/function61/gokit/os/osutil"
	"github.com/function61/gokit/os/systemdinstaller"
	"github.com/spf13/cobra"
)

type Options struct {
	ServiceName    string
	SubcommandName string
}

func (o Options) ConfigDir() erserver.ConfigDir {
	return erserver.ConfigDir(filepath.Join("/etc", o.ServiceName))
}

// suppose you embed Edgerouter in application called "bob".
// it is assumed that you'll assign top-level subcommand name e.g. "proxy" for Edgerouter inside bob.
// hence commands to edgerouter will start with "bob proxy".
func Entrypoint(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     opts.SubcommandName,
		Short:   "Edgerouter embeddable proxy",
		Version: dynversion.Version,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "Runs the HTTP server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := slog.Default()

			return erserver.Serve(
				cmd.Context(),
				opts.ConfigDir(),
				logger)
		},
	})

	cmd.AddCommand(setupDevCertsEntry(opts))

	cmd.AddCommand(&cobra.Command{
		Use:   "install-as-service",
		Short: "Install systemd service file to start Edgerouter on system startup",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return installAsService(opts)
		},
	})

	return cmd
}

func installAsService(opts Options) error {
	envFile := opts.ConfigDir().File("edgerouter.env")
	exists, err := osutil.Exists(opts.ConfigDir().String())
	if err != nil {
		return err
	}

	if exists {
		return fmt.Errorf("'%s' exists - Edgerouter already installed? Not continuing for safety", opts.ConfigDir().String())
	}

	service := systemdinstaller.Service(
		opts.ServiceName,
		"Edgerouter",
		systemdinstaller.Args(opts.SubcommandName, "serve"),
		// TODO: systemdinstaller.EnvFile("/etc/edgerouter.env")
		systemdinstaller.Env("DUMMY", "x\nEnvironmentFile="+envFile), // ugly hack
		systemdinstaller.Docs(
			"https://github.com/function61/edgerouter",
			"https://function61.com/"))

	if err := systemdinstaller.Install(service); err != nil {
		return err
	}

	fmt.Println(systemdinstaller.EnableAndStartCommandHints(service))

	if err := os.MkdirAll(opts.ConfigDir().String(), 0700); err != nil {
		return err
	}

	return os.WriteFile(envFile, []byte(`
# --- Docker integration
DOCKER_URL=unix:///var/run/docker.sock
NETWORK_NAME=docker_gwbridge

# --- CertBus / EventHorizon-based discovery / Lambda function routing / S3 hosting
# AWS_SECRET_ACCESS_KEY=...
# AWS_ACCESS_KEY_ID=AKIA...

# --- CertBus / EventHorizon-based discovery
# EVENTHORIZON=prod:1:::eu-central-1

# --- CertBus
# CERTBUS_CLIENT_PRIVKEY=...
`), 0600)
}
