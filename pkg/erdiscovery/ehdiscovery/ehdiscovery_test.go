package ehdiscovery

import (
	"context"
	"testing"
	"time"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdomain"
	"github.com/function61/eventhorizon/pkg/ehevent"
	"github.com/function61/eventhorizon/pkg/ehreader"
	"github.com/function61/eventhorizon/pkg/ehreader/ehreadertest"
	"github.com/function61/gokit/assert"
)

func TestDiscovery(t *testing.T) {
	ctx := context.Background()

	eventLog := ehreadertest.NewEventLog()
	eventLog.AppendE(
		"/t-42/loadbalancer",
		erdomain.NewAppUpdated(testApp("testApp1"), ehevent.MetaSystemUser(time.Now())))

	tenantCtx := ehreader.NewTenantCtx(ehreader.TenantId("42"), eventLog)

	discovery, err := New(*tenantCtx, nil)
	assert.Ok(t, err)

	apps, err := discovery.ReadApplications(ctx)
	assert.Ok(t, err)
	assert.Assert(t, len(apps) == 1)

	// create 2nd app

	eventLog.AppendE(
		"/t-42/loadbalancer",
		erdomain.NewAppUpdated(testApp("testApp2"), ehevent.MetaSystemUser(time.Now())))

	apps, err = discovery.ReadApplications(ctx)
	assert.Ok(t, err)
	assert.Assert(t, len(apps) == 2)

	// update existing app should not change # of apps

	eventLog.AppendE(
		"/t-42/loadbalancer",
		erdomain.NewAppUpdated(testApp("testApp1"), ehevent.MetaSystemUser(time.Now())))

	apps, err = discovery.ReadApplications(ctx)
	assert.Ok(t, err)
	assert.Assert(t, len(apps) == 2)

	// now delete app 2

	eventLog.AppendE(
		"/t-42/loadbalancer",
		erdomain.NewAppDeleted("testApp1", ehevent.MetaSystemUser(time.Now())))

	apps, err = discovery.ReadApplications(ctx)
	assert.Ok(t, err)
	assert.Assert(t, len(apps) == 1)
}

func testApp(id string) erconfig.Application {
	return erconfig.SimpleApplication(
		id,
		erconfig.SimpleHostnameFrontend("example.com", "/", false),
		erconfig.PeerSetBackend([]string{"http://127.0.0.1/"}, nil))
}
