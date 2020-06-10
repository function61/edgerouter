// EventHorizon-based discovery
package ehdiscovery

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery"
	"github.com/function61/edgerouter/pkg/erdomain"
	"github.com/function61/eventhorizon/pkg/ehclient"
	"github.com/function61/eventhorizon/pkg/ehevent"
	"github.com/function61/eventhorizon/pkg/ehreader"
	"github.com/function61/gokit/logex"
)

const (
	stream = "/loadbalancer"
)

type ehDiscovery struct {
	tenantCtx ehreader.TenantCtx
	reader    *ehreader.Reader
	cursor    ehclient.Cursor
	logl      *logex.Leveled
	apps      map[string]erconfig.Application
	appsMu    sync.Mutex
}

func New(tenantCtx ehreader.TenantCtx, logger *log.Logger) (erdiscovery.ReaderWriter, error) {
	d := &ehDiscovery{
		tenantCtx: tenantCtx,
		cursor:    ehclient.Beginning(tenantCtx.Stream(stream)),
		logl:      logex.Levels(logger),
		apps:      map[string]erconfig.Application{},
	}

	d.reader = ehreader.New(d, tenantCtx.Client, logger)

	return d, nil
}

func (d *ehDiscovery) ReadApplications(ctx context.Context) ([]erconfig.Application, error) {
	// this is essentially polling
	if err := d.reader.LoadUntilRealtime(ctx); err != nil {
		return nil, err
	}

	d.appsMu.Lock()
	defer d.appsMu.Unlock()

	apps := []erconfig.Application{}

	for _, app := range d.apps {
		apps = append(apps, app)
	}

	sort.Slice(apps, func(i, j int) bool { return apps[i].Id < apps[j].Id })

	return apps, nil
}

func (d *ehDiscovery) UpdateApplication(ctx context.Context, app erconfig.Application) error {
	updated := erdomain.NewAppUpdated(app, ehevent.MetaSystemUser(time.Now()))

	_, err := d.tenantCtx.Client.Append(ctx, d.tenantCtx.Stream(stream), []string{
		ehevent.Serialize(updated),
	})
	return err
}

func (d *ehDiscovery) DeleteApplication(ctx context.Context, app erconfig.Application) error {
	deleted := erdomain.NewAppDeleted(app.Id, ehevent.MetaSystemUser(time.Now()))

	_, err := d.tenantCtx.Client.Append(ctx, d.tenantCtx.Stream(stream), []string{
		ehevent.Serialize(deleted),
	})
	return err
}

func (d *ehDiscovery) GetEventTypes() ehevent.Allocators {
	return erdomain.Types
}

func (d *ehDiscovery) ProcessEvents(ctx context.Context, handle ehreader.EventProcessorHandler) error {
	d.appsMu.Lock()
	defer d.appsMu.Unlock()

	return handle(
		d.cursor,
		func(e ehevent.Event) error { return d.processEvent(e) },
		func(commitCursor ehclient.Cursor) error {
			d.cursor = commitCursor
			return nil
		},
	)
}

func (d *ehDiscovery) processEvent(ev ehevent.Event) error {
	d.logl.Info.Println(ev.MetaType())

	switch e := ev.(type) {
	case *erdomain.AppUpdated:
		d.apps[e.Application.Id] = e.Application
	case *erdomain.AppDeleted:
		delete(d.apps, e.Id)
	default:
		return ehreader.UnsupportedEventTypeErr(ev)
	}

	return nil
}
