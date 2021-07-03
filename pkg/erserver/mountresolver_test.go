package erserver

import (
	"testing"
	"time"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/gokit/assert"
)

func TestMountResolver(t *testing.T) {
	apps := []erconfig.Application{
		erconfig.SimpleApplication(
			"examplecom-app",
			erconfig.SimpleHostnameFrontend("example.com"),
			erconfig.RedirectBackend("http://example.net/1")),
		erconfig.SimpleApplication(
			"examplecom-docs-root",
			erconfig.SimpleHostnameFrontend("docs.example.com"),
			erconfig.RedirectBackend("http://example.net/2")),
		erconfig.SimpleApplication(
			"examplecom-docs-foo",
			erconfig.SimpleHostnameFrontend("docs.example.com", erconfig.PathPrefix("/foo")),
			erconfig.RedirectBackend("http://example.net/3")),
		erconfig.SimpleApplication(
			"examplecom-docs-bar",
			erconfig.SimpleHostnameFrontend("docs.example.com", erconfig.PathPrefix("/bar")),
			erconfig.RedirectBackend("http://example.net/4")),
		erconfig.SimpleApplication(
			"well-known",
			erconfig.PathPrefixFrontend("/.well-known/"), // intentionally introduce shorter path before longer /.well-known/acme-challenge/
			erconfig.RedirectBackend("http://example.net/6")),
		erconfig.SimpleApplication(
			"acme-challenge",
			erconfig.PathPrefixFrontend("/.well-known/acme-challenge/"),
			erconfig.RedirectBackend("http://example.net/5")),
	}

	matchers, err := appConfigToHandlersAndMatchers(apps, nil, time.Date(2021, 6, 30, 15, 17, 0, 0, time.UTC))
	assert.Ok(t, err)

	assert.Assert(t, resolveMount("notfound.net", "/", matchers) == nil)

	assert.EqualString(t, resolveMount("example.com", "/", matchers).App.Id, "examplecom-app")
	assert.EqualString(t, resolveMount("example.com", "/lollotilol", matchers).App.Id, "examplecom-app")

	assert.Assert(t, resolveMount("docs.example.com", "/", matchers).App.Id == "examplecom-docs-root")
	assert.EqualString(t, resolveMount("docs.example.com", "/foo", matchers).App.Id, "examplecom-docs-foo")
	assert.EqualString(t, resolveMount("docs.example.com", "/foo/", matchers).App.Id, "examplecom-docs-foo")
	assert.EqualString(t, resolveMount("docs.example.com", "/foo/stuff", matchers).App.Id, "examplecom-docs-foo")
	assert.EqualString(t, resolveMount("docs.example.com", "/foobar", matchers).App.Id, "examplecom-docs-root")
	assert.EqualString(t, resolveMount("docs.example.com", "/bar", matchers).App.Id, "examplecom-docs-bar")
	assert.EqualString(t, resolveMount("docs.example.com", "/.well-known/acme-challenge/TOKEN", matchers).App.Id, "acme-challenge")
	assert.EqualString(t, resolveMount("docs.example.com", "/.well-known/test", matchers).App.Id, "well-known")
}
