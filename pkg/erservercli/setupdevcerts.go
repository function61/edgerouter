package erservercli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/function61/gokit/osutil"
	"github.com/spf13/cobra"
)

func setupDevCertsEntry(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup-devcerts",
		Short: "mkcert utility shortcuts for generating CA and server cert for development purposes",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "ca-install",
		Short: "Install local-trust-only CA certificate into system trust stores",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			mkcert := exec.Command("mkcert", "-install")
			mkcert.Stdout = os.Stdout
			mkcert.Stderr = os.Stderr
			osutil.ExitIfError(translateIfMkcertNotInstalledError(mkcert.Run()))
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "servercert-generate [hostname]",
		Short: "Generate server cert. Example hostname: *.dev.example.com",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			osutil.ExitIfError(serverCertGenerate(args[0], opts))
		},
	})

	return cmd
}

func serverCertGenerate(hostname string, opts Options) error {
	tempDir, err := os.MkdirTemp("", "edgerouter-mkcert-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	if err := os.MkdirAll(filepath.Dir(opts.ConfigDir().DevelopmentCertificate()), 0750); err != nil {
		return translateIfSudoError(err)
	}

	// unfortunately, you can't ask mkcert where or for which name to store the certs under

	mkcert := exec.Command("mkcert", hostname)
	mkcert.Dir = tempDir
	mkcert.Stdout = os.Stdout
	mkcert.Stderr = os.Stderr
	if err := mkcert.Run(); err != nil {
		return translateIfMkcertNotInstalledError(err)
	}

	// *.dev.fn61.net would generate these names
	// _wildcard.dev.fn61.net.pem
	// _wildcard.dev.fn61.net-key.pem
	fmt.Println("\nServer cert generated.")

	key, err := findReadAndDeleteFile(filepath.Join(tempDir, "*-key.pem"))
	if err != nil {
		return err
	}

	cert, err := findReadAndDeleteFile(filepath.Join(tempDir, "*.pem"))
	if err != nil {
		return err
	}

	if err := translateIfSudoError(os.WriteFile(
		opts.ConfigDir().DevelopmentCertificate(),
		append(cert, key...),
		0600),
	); err != nil {
		return err
	}

	fmt.Printf(
		"Server cert written to '%s' - will be picked up on Edgerouter start\n",
		opts.ConfigDir().DevelopmentCertificate())

	return nil
}

func translateIfMkcertNotInstalledError(err error) error {
	if err != nil && errors.Is(err, exec.ErrNotFound) {
		return errors.New("mkcert not installed? See https://github.com/FiloSottile/mkcert#installation")
	}

	return err
}

func translateIfSudoError(err error) error {
	if err != nil && errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("(probably need '$ sudo ...') %w", err)
	}

	return err
}

func findReadAndDeleteFile(globPattern string) ([]byte, error) {
	globMatches, err := filepath.Glob(globPattern)
	if err != nil {
		return nil, err
	}

	if len(globMatches) != 1 {
		return nil, fmt.Errorf("findReadAndDeleteFile: expected 1 match; got %d", len(globMatches))
	}

	content, err := os.ReadFile(globMatches[0])
	if err != nil {
		return nil, err
	}

	if err := os.Remove(globMatches[0]); err != nil {
		return nil, err
	}

	return content, nil
}
