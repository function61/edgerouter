package erserver

import (
	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/gokit/assert"
	"testing"
)

func TestMain(t *testing.T) {
	apps := []erconfig.Application{
		erconfig.SimpleApplication(
			"examplecom-app",
			erconfig.SimpleHostnameFrontend("example.com", "/", false),
			erconfig.PeerSetBackend([]string{"127.0.0.1"}, nil)),
		erconfig.SimpleApplication(
			"examplecom-docs-root",
			erconfig.SimpleHostnameFrontend("docs.example.com", "/", false),
			erconfig.PeerSetBackend([]string{"127.0.0.2"}, nil)),
		erconfig.SimpleApplication(
			"examplecom-docs-foo",
			erconfig.SimpleHostnameFrontend("docs.example.com", "/foo", false),
			erconfig.PeerSetBackend([]string{"127.0.0.3"}, nil)),
		erconfig.SimpleApplication(
			"examplecom-docs-bar",
			erconfig.SimpleHostnameFrontend("docs.example.com", "/bar", false),
			erconfig.PeerSetBackend([]string{"127.0.0.4"}, nil)),
	}

	matchers, err := appsToFrontendMatchers(apps)
	assert.Assert(t, err == nil)

	assert.Assert(t, resolveMount("notfound.net", "/", matchers) == nil)

	assert.EqualString(t, resolveMount("example.com", "/", matchers).App.Id, "examplecom-app")
	assert.EqualString(t, resolveMount("example.com", "/lollotilol", matchers).App.Id, "examplecom-app")

	assert.Assert(t, resolveMount("docs.example.com", "/", matchers).App.Id == "examplecom-docs-root")
	assert.EqualString(t, resolveMount("docs.example.com", "/foo", matchers).App.Id, "examplecom-docs-foo")
	assert.EqualString(t, resolveMount("docs.example.com", "/foo/", matchers).App.Id, "examplecom-docs-foo")
	assert.EqualString(t, resolveMount("docs.example.com", "/foo/stuff", matchers).App.Id, "examplecom-docs-foo")
	assert.EqualString(t, resolveMount("docs.example.com", "/foobar", matchers).App.Id, "examplecom-docs-root")
	assert.EqualString(t, resolveMount("docs.example.com", "/bar", matchers).App.Id, "examplecom-docs-bar")
}
