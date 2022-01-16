package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery"
	"github.com/function61/edgerouter/pkg/erdiscovery/defaultdiscovery"
	"github.com/function61/gokit/jsonfile"
	"github.com/function61/gokit/osutil"
	"github.com/scylladb/termtables"
	"github.com/spf13/cobra"
)

func discoveryList() error {
	discoverySvc, err := newDefaultDiscoveryWithoutLogger()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
			app.Id,
			strings.Join(frontendDescrs, ", "),
			app.Backend.Describe())
	}

	fmt.Println(tbl.Render())

	return nil
}

func discoveryDeleteApplication(appId string) error {
	discoverySvc, err := newDefaultDiscoveryWithoutLogger()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	apps, err := discoverySvc.ReadApplications(ctx)
	if err != nil {
		return err
	}

	app := erconfig.FindApplication(appId, apps)
	if app == nil {
		return errors.New("app to delete not found")
	}

	return discoverySvc.DeleteApplication(ctx, *app)
}

func discoveryPut(content io.Reader, newOk bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

	if err := jsonfile.Unmarshal(content, app, true); err != nil {
		return err
	}

	if err := app.Validate(); err != nil {
		return err
	}

	appAlreadyExists := erconfig.FindApplication(app.Id, existingApps) != nil

	// sanity checks

	if newOk && appAlreadyExists {
		return errors.New("want to create new App but it already exists")
	}

	if !newOk && !appAlreadyExists {
		return errors.New("App doesn't exist. did you mean to create new with '--new'?")
	}

	return discoverySvc.UpdateApplication(ctx, *app)
}

func discoveryPutEntry() *cobra.Command {
	newOk := false

	cmd := &cobra.Command{
		Use:   "put",
		Short: "Update discovery config for application",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			osutil.ExitIfError(discoveryPut(os.Stdin, newOk))
		},
	}

	cmd.Flags().BoolVarP(&newOk, "new", "", newOk, "Ok to create as new application")

	return cmd
}

func discoveryCat(appId string) error {
	discoverySvc, err := newDefaultDiscoveryWithoutLogger()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	apps, err := discoverySvc.ReadApplications(ctx)
	if err != nil {
		return err
	}

	app := erconfig.FindApplication(appId, apps)
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
		Run: func(cmd *cobra.Command, args []string) {
			osutil.ExitIfError(discoveryList())
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "cat <appId>",
		Short: "Dump discovery config for application",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			osutil.ExitIfError(discoveryCat(args[0]))
		},
	})

	cmd.AddCommand(discoveryPutEntry())

	cmd.AddCommand(&cobra.Command{
		Use:   "rm [appId]",
		Short: "Delete application",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			osutil.ExitIfError(discoveryDeleteApplication(args[0]))
		},
	})

	return cmd
}

func newDefaultDiscoveryWithoutLogger() (erdiscovery.ReaderWriter, error) {
	return defaultdiscovery.New(nil)
}
