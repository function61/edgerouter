package erserver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery"
	"github.com/prometheus/client_golang/prometheus"
)

// end-to-end test for WebDAV correctly passing through
//
// main considerations to keep in mind:
// - reverse proxy MUST NOT do HTTP method allowlisting (or if does, it has to accept all method needed by WebDAV)
// - the requests passed to WebDAV origin SHOULD NOT be path translated, as file paths are also communicated via headers and whatnot
func TestReverseProxyWebDAV(t *testing.T) {
	t.Setenv("METRICS_ENDPOINT", "")
	t.Setenv("TURBOCHARGER_STORE", "")

	const (
		publicHost = "dav.example.com"
		username   = "alice"
		password   = "secret"
		fileBody   = "WebDAV integration test\n"
		lockToken  = "<opaquelocktoken:edgerouter-test>"
	)

	files := map[string]string{}
	webDAVOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != publicHost {
			http.Error(w, "unexpected Host: "+r.Host, http.StatusBadRequest)
			return
		}

		actualUsername, actualPassword, ok := r.BasicAuth()
		if !ok || actualUsername != username || actualPassword != password {
			http.Error(w, "missing origin credentials", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Forwarded-For") == "" {
			http.Error(w, "missing X-Forwarded-For", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodOptions:
			w.Header().Set("DAV", "1, 2")
			w.Header().Set("Allow", "OPTIONS, PROPFIND, PROPPATCH, MKCOL, PUT, COPY, MOVE, LOCK, UNLOCK, GET, DELETE")
			w.WriteHeader(http.StatusNoContent)
		case "MKCOL":
			if r.URL.Path != "/documents/" {
				http.Error(w, "unexpected collection path", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			files[r.URL.Path] = string(body)
			w.WriteHeader(http.StatusCreated)
		case "PROPFIND":
			if r.Header.Get("Depth") != "1" {
				http.Error(w, "unexpected Depth header", http.StatusBadRequest)
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || !bytes.Contains(body, []byte("propfind")) {
				http.Error(w, "unexpected PROPFIND body", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>https://%s/documents/source.txt</d:href></d:response></d:multistatus>`, publicHost)
		case "PROPPATCH":
			body, err := io.ReadAll(r.Body)
			if err != nil || !bytes.Contains(body, []byte("propertyupdate")) {
				http.Error(w, "unexpected PROPPATCH body", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"/>`)
		case "COPY", "MOVE":
			destination, err := url.Parse(r.Header.Get("Destination"))
			if err != nil || destination.Scheme != "https" || destination.Host != publicHost {
				http.Error(w, "unexpected Destination header", http.StatusBadRequest)
				return
			}
			if r.Header.Get("Overwrite") != "T" {
				http.Error(w, "unexpected Overwrite header", http.StatusBadRequest)
				return
			}
			files[destination.Path] = files[r.URL.Path]
			if r.Method == "MOVE" {
				delete(files, r.URL.Path)
			}
			w.WriteHeader(http.StatusCreated)
		case "LOCK":
			if r.Header.Get("Timeout") != "Second-3600" {
				http.Error(w, "unexpected Timeout header", http.StatusBadRequest)
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || !bytes.Contains(body, []byte("lockinfo")) {
				http.Error(w, "unexpected LOCK body", http.StatusBadRequest)
				return
			}
			w.Header().Set("Lock-Token", lockToken)
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			_, _ = io.WriteString(w, `<?xml version="1.0"?><d:prop xmlns:d="DAV:"/>`)
		case "UNLOCK":
			if r.Header.Get("Lock-Token") != lockToken {
				http.Error(w, "unexpected Lock-Token header", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			body, found := files[r.URL.Path]
			if !found {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, body)
		case http.MethodDelete:
			delete(files, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(webDAVOrigin.Close)

	app := erconfig.SimpleApplication(
		"webdav",
		erconfig.SimpleHostnameFrontend(publicHost),
		erconfig.ReverseProxyBackend([]string{webDAVOrigin.URL}, nil, true))
	currentConfig := newAtomicConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	matchers, err := syncAppsFromDiscovery(
		context.Background(),
		erdiscovery.StaticDiscovery([]erconfig.Application{app}),
		currentConfig,
		logger,
		logger)
	if err != nil {
		t.Fatal(err)
	}
	currentConfig.Store(matchers)

	metrics := initMetrics()
	t.Cleanup(func() {
		prometheus.Unregister(metrics.requestsOk)
		prometheus.Unregister(metrics.requestsFail)
		prometheus.Unregister(metrics.requestDuration)
	})
	edgeRouter := httptest.NewTLSServer(newServerHandler(currentConfig, nil, metrics))
	t.Cleanup(edgeRouter.Close)

	request := func(method string, path string, body string, headers map[string]string) *http.Response {
		t.Helper()

		req, err := http.NewRequest(method, edgeRouter.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = publicHost
		req.SetBasicAuth(username, password)
		for key, value := range headers {
			req.Header.Set(key, value)
		}

		resp, err := edgeRouter.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	assertResponse := func(resp *http.Response, expectedStatus int, expectedBody string) {
		t.Helper()
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != expectedStatus {
			t.Fatalf("expected status %d, got %d: %s", expectedStatus, resp.StatusCode, body)
		}
		if expectedBody != "" && !strings.Contains(string(body), expectedBody) {
			t.Fatalf("expected body to contain %q, got %q", expectedBody, body)
		}
	}

	options := request(http.MethodOptions, "/", "", nil)
	if options.Header.Get("DAV") != "1, 2" {
		t.Fatalf("expected DAV response header, got %q", options.Header.Get("DAV"))
	}
	assertResponse(options, http.StatusNoContent, "")

	assertResponse(request("MKCOL", "/documents/", "", nil), http.StatusCreated, "")
	assertResponse(request(http.MethodPut, "/documents/source.txt", fileBody, nil), http.StatusCreated, "")
	assertResponse(request("PROPFIND", "/documents/", `<d:propfind xmlns:d="DAV:"/>`, map[string]string{
		"Depth": "1",
	}), http.StatusMultiStatus, "https://"+publicHost+"/documents/source.txt")
	assertResponse(request("PROPPATCH", "/documents/source.txt", `<d:propertyupdate xmlns:d="DAV:"/>`, nil), http.StatusMultiStatus, "<d:multistatus")
	assertResponse(request("COPY", "/documents/source.txt", "", map[string]string{
		"Destination": "https://" + publicHost + "/documents/copied.txt",
		"Overwrite":   "T",
	}), http.StatusCreated, "")
	assertResponse(request("MOVE", "/documents/copied.txt", "", map[string]string{
		"Destination": "https://" + publicHost + "/documents/moved.txt",
		"Overwrite":   "T",
	}), http.StatusCreated, "")

	lock := request("LOCK", "/documents/moved.txt", `<d:lockinfo xmlns:d="DAV:"/>`, map[string]string{
		"Timeout": "Second-3600",
	})
	if lock.Header.Get("Lock-Token") != lockToken {
		t.Fatalf("expected Lock-Token response header, got %q", lock.Header.Get("Lock-Token"))
	}
	assertResponse(lock, http.StatusOK, "<d:prop")
	assertResponse(request("UNLOCK", "/documents/moved.txt", "", map[string]string{
		"Lock-Token": lockToken,
	}), http.StatusNoContent, "")
	assertResponse(request(http.MethodGet, "/documents/moved.txt", "", nil), http.StatusOK, fileBody)
	assertResponse(request(http.MethodDelete, "/documents/moved.txt", "", nil), http.StatusNoContent, "")
}
