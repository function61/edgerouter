package erserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/gokit/testing/assert"
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

	matchers, err := appConfigToHandlersAndMatchers(context.Background(), apps, nil, time.Date(2021, 6, 30, 15, 17, 0, 0, time.UTC), nil)
	assert.Ok(t, err)

	assert.Equal(t, resolveMount("notfound.net", "/", matchers) == nil, true)

	assert.Equal(t, resolveMount("example.com", "/", matchers).App.ID, "examplecom-app")
	assert.Equal(t, resolveMount("example.com", "/lollotilol", matchers).App.ID, "examplecom-app")

	assert.Equal(t, resolveMount("docs.example.com", "/", matchers).App.ID == "examplecom-docs-root", true)
	assert.Equal(t, resolveMount("docs.example.com", "/foo", matchers).App.ID, "examplecom-docs-foo")
	assert.Equal(t, resolveMount("docs.example.com", "/foo/", matchers).App.ID, "examplecom-docs-foo")
	assert.Equal(t, resolveMount("docs.example.com", "/foo/stuff", matchers).App.ID, "examplecom-docs-foo")
	assert.Equal(t, resolveMount("docs.example.com", "/foobar", matchers).App.ID, "examplecom-docs-root")
	assert.Equal(t, resolveMount("docs.example.com", "/bar", matchers).App.ID, "examplecom-docs-bar")
	assert.Equal(t, resolveMount("docs.example.com", "/.well-known/acme-challenge/TOKEN", matchers).App.ID, "acme-challenge")
	assert.Equal(t, resolveMount("docs.example.com", "/.well-known/test", matchers).App.ID, "well-known")
}

func TestGoImportFrontendUsesFirstProjectPath(t *testing.T) {
	apps := []erconfig.Application{
		erconfig.SimpleApplication(
			"website",
			erconfig.SimpleHostnameFrontend("example.com"),
			erconfig.RedirectBackend("https://example.org")),
		{
			ID: "go-imports",
			Frontends: []erconfig.Frontend{
				erconfig.SimpleHostnameFrontend("example.com", erconfig.PathPrefix("/mylib1")),
				erconfig.SimpleHostnameFrontend("example.com", erconfig.PathPrefix("/mylib2")),
			},
			Backend: erconfig.GoImportBackend("https://github.com/myorg"),
		},
	}

	matchers, err := appConfigToHandlersAndMatchers(context.Background(), apps, nil, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}

	mylib1 := resolveMount("example.com", "/mylib1/subpackage", matchers)
	if mylib1 == nil || mylib1.App.ID != "go-imports" {
		t.Fatalf("/mylib1/subpackage resolved to %#v", mylib1)
	}

	response := httptest.NewRecorder()
	mylib1.backend.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://example.com/mylib1/subpackage?go-get=1", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got, want := response.Body.String(), "example.com/mylib1 git https://github.com/myorg/mylib1"; !strings.Contains(got, want) {
		t.Errorf("response body %q does not contain %q", got, want)
	}

	if website := resolveMount("example.com", "/about", matchers); website == nil || website.App.ID != "website" {
		t.Fatalf("/about resolved to %#v", website)
	}
}
