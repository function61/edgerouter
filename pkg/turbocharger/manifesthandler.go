package turbocharger

// Content consumption - serves files from a known manifest ID.

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/function61/edgerouter/pkg/syncutil"
	"github.com/function61/gokit/logex"
)

type ManifestHandler struct {
	// this is the origin for files and manifests that we want to act as the cache for, i.e. minimize requests to these.
	originFilesAndManifests     CASPair
	originFileDownloadLocks     *syncutil.MutexMap
	originManifestDownloadLocks *syncutil.MutexMap

	// these fast caches will hold copies of things in *originFilesAndManifests*
	cacheGzipped      CAS // most of the files live here, transparently compressed so we can deliver compressed content with very little CPU
	cacheUncompressed CAS // for non-compressible files like .jpg, .mp4 etc. also caches manifests

	// fast RAM cache for resolving "this website version has these files" -queries
	cachedManifests   map[ObjectID]*optimizedManifest
	cachedManifestsMu sync.Mutex

	logl *logex.Leveled
}

func NewManifestHandlerAndStorages() (*ManifestHandler, error) {
	storages, err := StorageFromConfig()
	if err != nil {
		return nil, err
	}

	return newManifestHandler(*storages)
}

func newManifestHandler(originFilesAndManifests CASPair) (*ManifestHandler, error) {
	cacheGzipped, err := newFileStore("/var/cache/edgerouter/turbocharger/gzipped")
	if err != nil {
		return nil, fmt.Errorf("turbocharger: %w", err)
	}

	cacheUncompressed, err := newFileStore("/var/cache/edgerouter/turbocharger/uncompressed")
	if err != nil {
		return nil, fmt.Errorf("turbocharger: %w", err)
	}

	return newManifestHandlerWithCaches(originFilesAndManifests, cacheGzipped, cacheUncompressed), nil
}

// for testing
func newManifestHandlerWithCaches(originFilesAndManifests CASPair, cacheGzipped CAS, cacheUncompressed CAS) *ManifestHandler {
	return &ManifestHandler{
		originFilesAndManifests:     originFilesAndManifests,
		originFileDownloadLocks:     syncutil.NewMutexMap(),
		originManifestDownloadLocks: syncutil.NewMutexMap(),
		cacheGzipped:                cacheGzipped,
		cacheUncompressed:           cacheUncompressed,
		cachedManifests:             map[ObjectID]*optimizedManifest{},
		logl:                        logex.Levels(logex.Discard),
	}
}

func (h *ManifestHandler) ServeHTTPFromManifest(manifestID ObjectID, w http.ResponseWriter, r *http.Request) error {
	key := r.URL.Path // TODO: improve

	manifest, err := h.resolveManifest(manifestID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return err
	}

	file, status, err := func() (Path, int, error) {
		file, found := manifest.files[key]
		if !found { // we can gate all 404s here, i.e. below this all files are expected to be found from cache or origin
			if customNotFoundPage, customPageExists := manifest.files["/404.html"]; customPageExists {
				return customNotFoundPage, http.StatusNotFound, nil
			} else {
				http.NotFound(w, r)
				return Path{}, 0, fs.ErrNotExist // TODO: what semantics to apply to our error return?
			}
		} else {
			return file, http.StatusOK, nil
		}
	}()
	if err != nil {
		return err
	}

	ifNoneMatch := r.Header.Get("If-None-Match")

	if ifNoneMatch == file.ContentID.ETagUncompressed() || ifNoneMatch == file.ContentID.ETagGZipped() {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}

	// this (and the not modified above) are the expected 99.99 % happy path
	served, err := h.serveFromCache(file, status, w, r)
	if err != nil {
		return err
	}

	if served {
		return nil
	}

	// => cache miss (from both) -> fallback to serving from origin (+ try hydrating cache)

	return h.hydrateCacheFromOriginAndServeFromCache(file, status, w, r)
}

func (h *ManifestHandler) serveFromCache(file Path, status int, w http.ResponseWriter, r *http.Request) (bool, error) {
	sendToClient := func(bodyMaybeCompressed io.Reader, bodyIsCompressed bool) error {
		contentType := mime.TypeByExtension(filepath.Ext(file.Path))
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		w.Header().Set("Content-Type", contentType)

		// pipe through unchanged. this is expected to be our majority case that we have optimized for.
		// delivering compressed data doesn't take any additional CPU since we pre-compress the file before delivery.
		if bodyIsCompressed && clientSupportsGzip(r) {
			// can't use Transfer-Encoding because Go would add two Content-Encoding headers
			// (the other for chunked encoding). Transfer-Encoding would be ideal, but in real world
			// we seem to need to use Content-Encoding.
			// https://stackoverflow.com/questions/11641923/transfer-encoding-gzip-vs-content-encoding-gzip
			// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Content-Encoding
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("ETag", file.ContentID.ETagGZipped())
			w.WriteHeader(status)

			_, err := io.Copy(w, bodyMaybeCompressed)
			return err
		} else { // body is not compressed or client doesn't support gzip.
			// in both cases we pump uncompressed data to client, we just might need to decompress.
			bodyDefinitelyUncompressed, err := func() (io.Reader, error) {
				if bodyIsCompressed {
					return gzip.NewReader(bodyMaybeCompressed)
				} else {
					return bodyMaybeCompressed, nil // as-is
				}
			}()
			if err != nil {
				return err
			}

			w.Header().Set("ETag", file.ContentID.ETagUncompressed())
			w.WriteHeader(status)

			_, err = io.Copy(w, bodyDefinitelyUncompressed)
			return err
		}
	}

	// if we have the file cached, it lives in either the compressed cache or the uncompressed cache
	// (NOT both). most of the files are expected to compress well, so so we try GZIP cache first

	gzipped, err := h.cacheGzipped.GetObject(r.Context(), file.ContentID)
	if err == nil { // found => serve from gzip
		defer gzipped.Close()

		return true, sendToClient(gzipped, true)
	} else if err != fs.ErrNotExist { // an actual error with the cache => serve from origin
		h.logl.Error.Printf("cacheGzipped: %v", err)
	}

	// => gzipped cache miss -> try from uncompressed cache

	uncompressed, err := h.cacheUncompressed.GetObject(r.Context(), file.ContentID)
	if err == nil { // found => serve uncompressed (this is uncompressible data like images or videos)
		defer uncompressed.Close()

		return true, sendToClient(uncompressed, false)
	} else if err != fs.ErrNotExist { // an actual error with the cache => serve from origin
		h.logl.Error.Printf("cacheUncompressed: %v", err)
	}

	// => missed both caches

	return false, nil
}

// this is expected to be a relatively rare event
func (h *ManifestHandler) hydrateCacheFromOriginAndServeFromCache(file Path, status int, w http.ResponseWriter, r *http.Request) error {
	// we don't want multiple people to start downloading the same file from the origin at once,
	// so we'll use a mutex map. it wouldn't be very dangerous (inserts to cache CAS are atomic), but
	// on very high traffic servers that causes a sudden rush of requests to the origin.
	//
	// we could in theory tee the download from origin to the first requesting client and the cache
	// so the first user would see better latency ..:
	//
	//            ┌────► Client
	// Origin ────┤
	//            └────► Cache
	//
	// .. but a malicious first-user could have artifially low download speed and thus make the other
	// queued clients wait. So we'll just fill cache first at origin -> cache server speed and when
	// that is done, start serving the queued clients. This has higher time-to-first-byte for the very
	// first time someone downloads the file, but is safer and makes the code simpler (reuse serveFromCache).

	err := func() error { // function to limit lock lifetime
		/*
			only one download for the same file.
			the first consumer to get a lock is responsible for cache hydration (which the rest will simply wait on).
			this is not perfect as after unlocking there can be still consumers racing to TryLock()
			who'll get wasFirst=true which will trigger additional hydrations (but that's not dangerous).

			this was validated with a Hey run: $ hey -n 20000 http://localhost:8080/19-C6M5Z7KR.jpg

			Response time histogram:
			  0.000 [1]     |
			  0.042 [19949] |■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■
			  0.084 [0]     |
			  0.126 [0]     |
			  0.167 [0]     |
			  0.209 [0]     |
			  0.251 [0]     |
			  0.293 [0]     |
			  0.335 [0]     |
			  0.377 [0]     |
			  0.418 [50]    |

			=> The first 50 (= concurrency default) requests took ~0.4 s while the last 19950 took <= 42 ms
		*/
		fileDownloadUnlock, wasFirst := h.originFileDownloadLocks.TryLock(file.ContentID.String())
		if !wasFirst {
			// wasn't first -> definitely wait for the cache hydration to finish
			defer h.originFileDownloadLocks.Lock(file.ContentID.String())() // also unlock when returning

			return nil
		}
		defer fileDownloadUnlock()

		// we were the first one to get a lock => we're responsible for hydrating the cache

		contentOriginal, err := h.originFilesAndManifests.Files.GetObject(context.Background(), file.ContentID)
		if err != nil {
			return err // "not exists" shouldn't ever happen, because manifest said the file would be here
		}
		defer contentOriginal.Close()

		// either insert into the gzipped cache or the uncompressed cache
		contentMaybeCompressed, cacheToUse, gzipWriterDone := func() (io.Reader, CAS, <-chan error) {
			gzipWriterDone := make(chan error, 1)

			if isExpectedToCompressWell(file.Path) {
				gzippedReader, sendToGzippedReader := io.Pipe()

				go func() {
					gzipWriterDone <- func() error {
						compress := gzip.NewWriter(sendToGzippedReader)
						if _, err = io.Copy(compress, contentOriginal); err != nil { // bulk of the compression
							return err
						}
						if err := compress.Close(); err != nil { // writes gzip footer
							return err
						}
						return sendToGzippedReader.Close() // sends EOF to to *gzippedReader*
					}()
				}()

				return gzippedReader, h.cacheGzipped, gzipWriterDone
			} else {
				gzipWriterDone <- nil
				return contentOriginal, h.cacheUncompressed, gzipWriterDone
			}
		}()

		if err := cacheToUse.InsertObject(context.Background(), file.ContentID, contentMaybeCompressed, "dummy"); err != nil {
			<-gzipWriterDone
			return err
		}

		return <-gzipWriterDone
	}()
	if err != nil {
		return err
	}

	served, err := h.serveFromCache(file, status, w, r)
	if err != nil {
		return err
	}

	if !served {
		return errors.New("cache hydration failed")
	}

	return nil
}

// finds the file list that tells us which named files our file tree contains
func (h *ManifestHandler) resolveManifest(manifestID ObjectID) (*optimizedManifest, error) {
	// first check if manifest is already in in-RAM cache
	manifestOptimized, manifestInRAM, manifestSpecificMutexRelease := func() (*optimizedManifest, bool, func()) {
		h.cachedManifestsMu.Lock()
		defer h.cachedManifestsMu.Unlock()

		manifestOptimized, found := h.cachedManifests[manifestID]
		if found { // happy 99.99 % path
			return manifestOptimized, true, nil
		}

		// manifest was not in manifestInRAM. take out a named lock to prevent competing downloads for the same manifest.
		manifestSpecificMutexRelease, gotLock := h.originManifestDownloadLocks.TryLock(manifestID.String())
		if gotLock { // we're the first one starting to download the manifest
			return nil, false, manifestSpecificMutexRelease
		} else { // someone else already downloading the manifest => wait for lock to be released and then re-try
			return nil, false, nil
		}
	}()
	if manifestInRAM { // happy 99.99 % path
		return manifestOptimized, nil
	}

	if manifestSpecificMutexRelease == nil { // someone is already downloading this manifest
		// wait until previous producer has a result
		unlock := h.originManifestDownloadLocks.Lock(manifestID.String())
		unlock() // it has now either succeeded or failed

		return h.resolveManifest(manifestID) // retry by re-entering this func
	}
	defer manifestSpecificMutexRelease()

	// slow path for manifest still being unknown to our RAM-based cache.
	// there are no competing producers for the same manifest below this line.

	manifest, err := func() (*Manifest, error) {
		// try first from cache (covers loadbalancer restarts without needing access to origin)
		manifest, err := h.cacheUncompressed.GetObject(context.Background(), manifestID)
		switch {
		case err == nil: // fast-path
			defer manifest.Close()

			return DecodeManifest(manifest)
		case !errors.Is(err, fs.ErrNotExist): // actually unexpected error
			h.logl.Error.Printf("resolveManifest cacheUncompressed: %v", err)
		}

		// then read from manifest origin (and remember to hydrate cache)
		manifest, err = h.originFilesAndManifests.Manifests.GetObject(context.Background(), manifestID)
		if err != nil {
			return nil, err
		}
		defer manifest.Close()

		// need to buffer because we need to read this twice
		manifestBuf, err := io.ReadAll(manifest)
		if err != nil {
			return nil, err
		}

		// hydrate cache
		if err := h.cacheUncompressed.InsertObject(context.Background(), manifestID, bytes.NewReader(manifestBuf), "application/json"); err != nil {
			h.logl.Error.Printf("resolveManifest cacheUncompressed: %v", err)
		}

		return DecodeManifest(bytes.NewReader(manifestBuf))
	}()
	if err != nil {
		return nil, err
	}

	manifestOptimized = optimizeManifest(*manifest)

	h.cachedManifestsMu.Lock()
	defer h.cachedManifestsMu.Unlock()

	h.cachedManifests[manifestID] = manifestOptimized

	return manifestOptimized, nil
}

// pre-computed lookup of Path objects by path:
type optimizedManifest struct {
	files map[string]Path // ["/index.html"] = Path{Path:"/index.html",ObjectID:"..."}
}

func optimizeManifest(manifest Manifest) *optimizedManifest {
	files := make(map[string]Path, len(manifest.Files))
	for _, file := range manifest.Files {
		files[file.Path] = file
	}

	return &optimizedManifest{files}
}

func isExpectedToCompressWell(key string) bool {
	// TODO: use mime database of text/* prefix?
	switch path.Ext(key) {
	case ".js", ".map", ".css", ".html", ".json", ".xml", ".svg", ".txt":
		return true
	default:
		return false
	}
}

func clientSupportsGzip(r *http.Request) bool {
	// FIXME: this is hacky
	return strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
}
