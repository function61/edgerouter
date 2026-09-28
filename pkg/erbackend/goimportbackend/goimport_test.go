package goimportbackend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/function61/edgerouter/pkg/erconfig"
)

func TestGoGetResponseUsesFirstPathComponentAsModuleRoot(t *testing.T) {
	handler := newBackend(t)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://example.com/mylib1/subpackage?go-get=1", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	if contentType := response.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", contentType)
	}

	const want = "<meta name=\"go-import\" content=\"example.com/mylib1 git https://github.com/myorg/mylib1\">"
	if body := response.Body.String(); !strings.Contains(body, want) {
		t.Errorf("response body %q does not contain %q", body, want)
	}
}

func TestBrowserRequestsRedirectTemporarilyToRepository(t *testing.T) {
	handler := newBackend(t)

	tests := []struct {
		name string
		path string
		want string
	}{
		{"module root", "/mylib1", "https://github.com/myorg/mylib1"},
		{"subpackage and query", "/mylib1/subpackage?view=all", "https://github.com/myorg/mylib1/subpackage?view=all"},
		{"go-get not one", "/mylib1?go-get=0", "https://github.com/myorg/mylib1?go-get=0"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://example.com"+test.path, nil))

			if response.Code != http.StatusFound {
				t.Errorf("status = %d, want %d", response.Code, http.StatusFound)
			}

			if location := response.Header().Get("Location"); location != test.want {
				t.Errorf("Location = %q, want %q", location, test.want)
			}
		})
	}
}

func TestNewRejectsMissingOptions(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProjectIsRequired(t *testing.T) {
	handler := newBackend(t)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://example.com/?go-get=1", nil))

	if response.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func newBackend(t *testing.T) http.Handler {
	t.Helper()

	backend, err := New(&erconfig.BackendOptsGoImport{RepositoryURLPrefix: "https://github.com/myorg"})
	if err != nil {
		t.Fatal(err)
	}

	return backend
}
