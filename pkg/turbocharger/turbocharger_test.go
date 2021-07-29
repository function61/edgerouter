package turbocharger

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/function61/gokit/assert"
)

func TestTurbocharger(t *testing.T) {
	files, manifests, cacheGzipped, cacheUncompressed := newInMemoryStore(), newInMemoryStore(), newInMemoryStore(), newInMemoryStore()

	newSnapshot := func() snapshot {
		return snapshot{
			files:             files.counters,
			manifests:         manifests.counters,
			cacheGzipped:      cacheGzipped.counters,
			cacheUncompressed: cacheUncompressed.counters,
		}
	}

	storages := CASPair{
		Files:     files,
		Manifests: manifests,
	}

	deployer := NewDeploymentManager(storages, nil)

	filesToUpload := []*FileToDeploy{
		&FileToDeploy{"/foo.txt", strings.NewReader("hello world")},
		&FileToDeploy{"/bar.jpg", bytes.NewReader([]byte{0x00, 0x01, 0x02})}, // should go in uncompressed cache
	}

	uploadFiles := func() *ManifestWithID {
		idx := 0
		man, err := deployer.Deploy(context.Background(), NewMetadata("testproject"), func() (*FileToDeploy, error) {
			defer func() { idx++ }()

			if idx < len(filesToUpload) {
				return filesToUpload[idx], nil
			} else {
				return nil, nil // eof
			}
		})
		assert.Ok(t, err)
		return man
	}

	man := uploadFiles()

	getRequest := func(target string) *http.Request { // shorthand
		return httptest.NewRequest(http.MethodGet, target, nil)
	}

	storagesAccessed := func(before snapshot, expected string) {
		t.Helper()

		now := newSnapshot()

		type compareItem struct {
			label    string
			counters opCounters
		}

		noteworthy := []string{} // where > 0

		for _, item := range []compareItem{
			{"files", now.files.subtract(before.files)},
			{"manifests", now.manifests.subtract(before.manifests)},
			{"cacheGzipped", now.cacheGzipped.subtract(before.cacheGzipped)},
			{"cacheUncompressed", now.cacheUncompressed.subtract(before.cacheUncompressed)},
		} {
			if item.counters.gets > 0 {
				noteworthy = append(noteworthy, fmt.Sprintf("%s.gets=%d", item.label, item.counters.gets))
			}

			if item.counters.puts > 0 {
				noteworthy = append(noteworthy, fmt.Sprintf("%s.puts=%d", item.label, item.counters.puts))
			}
		}

		assert.EqualString(t, strings.Join(noteworthy, ","), expected)
	}

	mh := newManifestHandlerWithCaches(storages, cacheGzipped, cacheUncompressed)

	// fetch initial manifest
	{
		before := newSnapshot()

		response := httptest.NewRecorder()

		// ask for a file that does not exist (we will still get manifest downloaded)
		assert.Assert(t, mh.ServeHTTPFromManifest(man.ID, response, getRequest("/does-not-exist.txt")) == fs.ErrNotExist)
		assert.Assert(t, response.Code == http.StatusNotFound)

		// manifest was checked from cache and retrieved from manifest origin and then cached
		storagesAccessed(before, "manifests.gets=1,cacheUncompressed.gets=1,cacheUncompressed.puts=1")
	}

	// manifest is in RAM cache
	{
		before := newSnapshot()

		response := httptest.NewRecorder()

		assert.Assert(t, mh.ServeHTTPFromManifest(man.ID, response, getRequest("/does-not-exist.txt")) == fs.ErrNotExist)
		assert.Assert(t, response.Code == http.StatusNotFound)

		// no fetches to backing store
		storagesAccessed(before, "")
	}

	// simulate restarting a loadbalancer process. we'll lose manifest RAM cache ..
	mh = newManifestHandlerWithCaches(storages, cacheGzipped, cacheUncompressed)

	// .. but will be able to load manifest from cache without having to contact manifest origin
	{
		before := newSnapshot()

		assert.Assert(t, mh.ServeHTTPFromManifest(man.ID, httptest.NewRecorder(), getRequest("/does-not-exist.txt")) == fs.ErrNotExist)

		storagesAccessed(before, "cacheUncompressed.gets=1")
	}

	// now fetch a file that exists but is not already in our cache
	{
		before := newSnapshot()

		response := httptest.NewRecorder()

		assert.Ok(t, mh.ServeHTTPFromManifest(man.ID, response, getRequest("/foo.txt")))
		assert.EqualString(t, response.Body.String(), "hello world")

		storagesAccessed(before, "files.gets=1,cacheGzipped.gets=2,cacheGzipped.puts=1,cacheUncompressed.gets=1")
	}

	// fetch same file again. it's now cached
	{
		before := newSnapshot()

		response := httptest.NewRecorder()

		assert.Ok(t, mh.ServeHTTPFromManifest(man.ID, response, getRequest("/foo.txt")))
		assert.EqualString(t, response.Header().Get("Content-Type"), "text/plain; charset=utf-8")
		assert.EqualString(t, response.Body.String(), "hello world")

		storagesAccessed(before, "cacheGzipped.gets=1")
	}

	// fetch new file, but one that is uncompressible
	{
		before := newSnapshot()

		response := httptest.NewRecorder()

		assert.Ok(t, mh.ServeHTTPFromManifest(man.ID, response, getRequest("/bar.jpg")))
		assert.EqualString(t, response.Header().Get("Content-Type"), "image/jpeg")
		assert.Assert(t, bytes.Equal(response.Body.Bytes(), []byte{0x00, 0x01, 0x02}))

		storagesAccessed(before, "files.gets=1,cacheGzipped.gets=2,cacheUncompressed.gets=2,cacheUncompressed.puts=1")
	}

	const barJpgETag = `"rksygOVuL6-D9BSm49q-nV--GJdlRMBf7RIazLhbU_w"`

	// fetching a cached uncompressed file should yield 1) cacheGzipped miss 2) cacheUncompressed hit
	{
		before := newSnapshot()

		response := httptest.NewRecorder()

		assert.Ok(t, mh.ServeHTTPFromManifest(man.ID, response, getRequest("/bar.jpg")))
		assert.Assert(t, bytes.Equal(response.Body.Bytes(), []byte{0x00, 0x01, 0x02}))

		assert.EqualString(t, response.Header().Get("ETag"), barJpgETag)

		storagesAccessed(before, "cacheGzipped.gets=1,cacheUncompressed.gets=1")
	}

	// test client-side caching
	{
		before := newSnapshot()

		response := httptest.NewRecorder()

		req := getRequest("/bar.jpg")
		req.Header.Set("If-None-Match", barJpgETag)

		assert.Ok(t, mh.ServeHTTPFromManifest(man.ID, response, req))
		assert.Assert(t, response.Code == http.StatusNotModified)
		assert.Assert(t, len(response.Body.Bytes()) == 0)

		storagesAccessed(before, "")
	}

	// TODO: incorrect manifest ID

	// add 404 page
	filesToUpload = append(filesToUpload, &FileToDeploy{"/404.html", strings.NewReader("pixels not found")})

	man = uploadFiles()

	// warm up the cache with new manifest etc. (also fetch 404 page into cache)
	assert.Ok(t, mh.ServeHTTPFromManifest(man.ID, httptest.NewRecorder(), getRequest("/does-not-exist.txt")))

	{
		before := newSnapshot()

		response := httptest.NewRecorder()

		assert.Ok(t, mh.ServeHTTPFromManifest(man.ID, response, getRequest("/does-not-exist.txt")))
		assert.Assert(t, response.Code == http.StatusNotFound)
		assert.EqualString(t, response.Body.String(), "pixels not found")

		// even custom 404s must be really cheap
		storagesAccessed(before, "cacheGzipped.gets=1")
	}

	incorrectManifestID := ObjectID([32]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x30, 0x31, 0x32})

	{
		before := newSnapshot()

		response := httptest.NewRecorder()

		assert.Assert(t, mh.ServeHTTPFromManifest(incorrectManifestID, response, getRequest("/does-not-exist.txt")) == fs.ErrNotExist)
		assert.Assert(t, response.Code == http.StatusInternalServerError)

		storagesAccessed(before, "manifests.gets=1,cacheUncompressed.gets=1")
	}
}

type snapshot struct {
	files             opCounters
	manifests         opCounters
	cacheGzipped      opCounters
	cacheUncompressed opCounters
}
