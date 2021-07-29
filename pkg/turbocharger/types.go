package turbocharger

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// represents S3, a filesystem or similar
type CAS interface {
	GetObject(ctx context.Context, id ObjectID) (io.ReadCloser, error)

	InsertObject(ctx context.Context, id ObjectID, content io.Reader, contentType string) error
}

// while you could use just one CAS to store both Files and Manifests, we suggest having separate
// stores for both so it's easier to enumerate deployments (= different manifest files) for cleanup
// purposes.
//
// suppose your AWS S3 bills are at pain limit and you'd like to delete no-longer-in-use files from
// the CAS. you could do this by enumerating deployments, grouping them by project and assessing by
// deployment timestamp which manifests to delete. then you'd clean up all files no longer
// referenced by any remaining manifests.
//
// we don't implement said pruning right now, but our architecture is prepared to patch that in later.
type CASPair struct {
	Files     CAS
	Manifests CAS
}

type ObjectID [sha256.Size]byte

func (o *ObjectID) String() string {
	return base64.RawURLEncoding.EncodeToString((*o)[:])
}

// ETag is supposed to be different if it has Content-Encoding applied
func (o *ObjectID) ETagGZipped() string {
	return fmt.Sprintf(`"%s-gz"`, o.String())
}

func (o *ObjectID) ETagUncompressed() string {
	return fmt.Sprintf(`"%s"`, o.String())
}

var _ interface {
	fmt.Stringer
	json.Marshaler
	json.Unmarshaler
} = (*ObjectID)(nil)

func (o *ObjectID) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, o.String())), nil
}

func (o *ObjectID) UnmarshalJSON(b []byte) error {
	temp := ""
	if err := json.Unmarshal(b, &temp); err != nil {
		return err
	}

	temp2, err := base64.RawURLEncoding.DecodeString(temp)
	if err != nil {
		return err
	}

	if copy((*o)[:], temp2) != len(o) {
		return errors.New("incorrect length")
	}

	return nil
}

func ObjectIDFromString(serialized string) (*ObjectID, error) {
	id := ObjectID{}
	if err := json.Unmarshal([]byte(fmt.Sprintf(`"%s"`, serialized)), &id); err != nil {
		return nil, err
	}

	return &id, nil
}

// is used to give a piece of content a name. file deduplication is achieved by same ContentID
// simply having multiple paths.
type Path struct {
	Path      string   `json:"path"`
	ContentID ObjectID `json:"id"`
}

// additional metadata not used by turbocharger itself, but can be later used to implement pruning
type ManifestMetadata struct {
	Project  string    `json:"project"`
	Deployed time.Time `json:"deployed"`
}

func NewMetadata(project string) ManifestMetadata {
	return ManifestMetadata{
		Project:  project,
		Deployed: time.Now().UTC(),
	}
}

type Manifest struct {
	Metadata ManifestMetadata `json:"metadata"`
	Files    []Path           `json:"files"` // "foobar/index.html" => 9f86d081884c7d65...
}

func decodeManifest(reader io.Reader) (*Manifest, error) {
	man := &Manifest{}
	if err := json.NewDecoder(reader).Decode(man); err != nil {
		return nil, fmt.Errorf("decodeManifest: %w", err)
	}

	return man, nil
}
