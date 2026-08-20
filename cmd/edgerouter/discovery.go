package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery"
	"github.com/function61/edgerouter/pkg/erdiscovery/defaultdiscovery"
	"github.com/function61/gokit/encoding/jsonfile"
	"github.com/scylladb/termtables"
	"github.com/spf13/cobra"
)

func discoveryList(ctx context.Context) error {
	discoverySvc, err := newDefaultDiscoveryWithoutLogger()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	apps, err := discoverySvc.ReadApplications(ctx)
	if err != nil {
		return err
	}

	tbl := termtables.CreateTable()
	tbl.AddHeaders("ID", "Frontends", "Backend")

	for _, app := range apps {
		frontendDescrs := []string{}

		for _, f := range app.Frontends {
			frontendDescrs = append(frontendDescrs, f.Describe())
		}

		tbl.AddRow(
			app.ID,
			strings.Join(frontendDescrs, ", "),
			app.Backend.Describe())
	}

	fmt.Println(tbl.Render())

	return nil
}

func discoveryDeleteApplication(ctx context.Context, appID string) error {
	discoverySvc, err := newDefaultDiscoveryWithoutLogger()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	apps, err := discoverySvc.ReadApplications(ctx)
	if err != nil {
		return err
	}

	app := erconfig.FindApplication(appID, apps)
	if app == nil {
		return errors.New("app to delete not found")
	}

	return discoverySvc.DeleteApplication(ctx, *app)
}

func discoveryPut(ctx context.Context, content io.Reader, newOk bool) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	discoverySvc, err := newDefaultDiscoveryWithoutLogger()
	if err != nil {
		return err
	}

	existingApps, err := discoverySvc.ReadApplications(ctx)
	if err != nil {
		return err
	}

	app := &erconfig.Application{}

	if err := jsonfile.UnmarshalDisallowUnknownFields(content, app); err != nil {
		return err
	}

	if err := app.Validate(); err != nil {
		return err
	}

	appAlreadyExists := erconfig.FindApplication(app.ID, existingApps) != nil

	// sanity checks

	if newOk && appAlreadyExists {
		return errors.New("want to create new App but it already exists")
	}

	if !newOk && !appAlreadyExists {
		return errors.New("app doesn't exist. did you mean to create new with '--new'?")
	}

	return discoverySvc.UpdateApplication(ctx, *app)
}

func discoveryPutEntry() *cobra.Command {
	newOk := false

	cmd := &cobra.Command{
		Use:   "put",
		Short: "Update discovery config for application",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return discoveryPut(cmd.Context(), os.Stdin, newOk)
		},
	}

	cmd.Flags().BoolVarP(&newOk, "new", "", newOk, "Ok to create as new application")

	return cmd
}

func discoveryCat(ctx context.Context, appID string) error {
	discoverySvc, err := newDefaultDiscoveryWithoutLogger()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	apps, err := discoverySvc.ReadApplications(ctx)
	if err != nil {
		return err
	}

	app := erconfig.FindApplication(appID, apps)
	if app == nil {
		return errors.New("app not found")
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(app)
}

func discoveryEntry() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discovery",
		Short: "Discovery related commands",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "Lists applications",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return discoveryList(cmd.Context())
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "cat <appId>",
		Short: "Dump discovery config for application",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return discoveryCat(cmd.Context(), args[0])
		},
	})

	cmd.AddCommand(discoveryPutEntry())

	cmd.AddCommand(&cobra.Command{
		Use:   "rm [appId]",
		Short: "Delete application",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return discoveryDeleteApplication(cmd.Context(), args[0])
		},
	})

	return cmd
}

func newDefaultDiscoveryWithoutLogger() (erdiscovery.ReaderWriter, error) {
	return defaultdiscovery.New(slog.New(slog.DiscardHandler))
}
