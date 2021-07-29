package turbocharger

// Things done at the time when content is "deployed" into CAS, i.e. relatively rare event when
// apps/websites are changed.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"path/filepath"
	"sort"
	"sync"

	"github.com/function61/edgerouter/pkg/syncutil"
	"github.com/function61/gokit/logex"
)

type FileToDeploy struct {
	Path    string // begins with /, e.g. "/index.html"
	Content io.Reader
}

// purely computed thing based on Manifest content
type ManifestWithID struct {
	ID       ObjectID
	Manifest Manifest
}

type deploymentManager struct {
	storages CASPair
	logl     *logex.Leveled
}

func NewDeploymentManager(storages CASPair, logger *log.Logger) *deploymentManager {
	return &deploymentManager{storages, logex.Levels(logger)}
}

// deploys files by inserting them into a CAS. you'll get back a manifest ID (which is found from manifest CAS)
func (d *deploymentManager) Deploy(
	ctx context.Context,
	metadata ManifestMetadata,
	nextFile func() (*FileToDeploy, error),
) (*ManifestWithID, error) {
	var manifestMu sync.Mutex
	manifest := Manifest{
		Metadata: metadata,
		Files:    []Path{},
	}

	type workItem struct {
		buf  []byte
		file *FileToDeploy
	}

	work := make(chan workItem)

	if err := syncutil.Concurrently(context.Background(), 3, func(ctx context.Context) error {
		for item := range work {
			contentID := calculateContentID(item.buf)

			d.logl.Info.Printf("uploading %s", item.file.Path)

			contentType := mime.TypeByExtension(filepath.Ext(item.file.Path))
			if contentType == "" {
				contentType = "application/octet-stream"
			}

			if err := d.storages.Files.InsertObject(ctx, contentID, bytes.NewReader(item.buf), contentType); err != nil {
				return err
			}

			func() {
				manifestMu.Lock()
				defer manifestMu.Unlock()

				manifest.Files = append(manifest.Files, Path{
					Path:      item.file.Path,
					ContentID: contentID,
				})
			}()
		}

		return nil
	}, func(workersCancel context.Context) error {
		defer close(work)

		for {
			file, err := nextFile()
			if err != nil {
				return err
			}
			if file == nil { // no more files
				return nil
			}

			// need to buffer because it's the easiest way to read the content twice (hashing + upload).
			// need to do this in single-threaded b/c for example tar reading does not support random access.
			buf, err := io.ReadAll(file.Content)
			if err != nil {
				return err
			}

			select {
			case work <- workItem{
				buf:  buf,
				file: file,
			}:
			case <-workersCancel.Done():
				return workersCancel.Err()
			}
		}
	}); err != nil {
		return nil, err
	}

	if len(manifest.Files) == 0 {
		return nil, errors.New("deployment doesn't have files")
	}

	// stable order, so if we upload same file set multiple times we end up with same manifest
	// (if all else, like metadata, is also equal)
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })

	manifestSerialized, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}

	manifestID := calculateContentID(manifestSerialized)

	if err := d.storages.Manifests.InsertObject(ctx, manifestID, bytes.NewReader(manifestSerialized), "application/json"); err != nil {
		return nil, err
	}

	return &ManifestWithID{
		ID:       manifestID,
		Manifest: manifest,
	}, nil
}

func calculateContentID(input []byte) ObjectID {
	digest := sha256.Sum256(input)

	id := ObjectID{}
	if copy(id[:], digest[:]) != len(id) {
		panic("short read") // shouldn't happen
	}

	return id
}
