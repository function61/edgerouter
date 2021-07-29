package turbocharger

// Intended to be used in a loadbalancer that fronts a backing application (that uses the appsupport
// library to opt-in to turbocharging). Automatic lazy discovery + serving of turbocharged subtrees
// from a tiered CAS. Tiers are:
//
//   1) fast local cache
//   2) origin)

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/function61/gokit/logex"
)

const (
	turbochargerAdvertisementHeaderKey = "turbocharger"
)

// meant to be used in front of origin, to serve origin's sub-tree (e.g. /static) faster via turbocharger
type turbochargerMiddleware struct {
	manifestHandler *ManifestHandler // HTTP handler that needs to know which manifest it's serving from
	origin          http.Handler     // a full web application

	discovered atomic.Value // *discoveredSubtree

	logl *logex.Leveled
}

// describes origin's subtree (e.g. /static and its version's files) for one validity period (such as 5 seconds).
// when validity period expires, the latest-discovered discovery result is used until the ping check finishes which either:
// a) creates a new discovery result with up-to-date details and validity period OR
// b) detaches turbocharger on error
type discoveredSubtree struct {
	originTurbocharged http.Handler       // faster subset of origin, i.e. only its /static/..., strips prefix before passing handling to *manifestHandler*
	subtreeVersion     turbochargeSubtree // subtree at a specific version. used to tell *manifestHandler* which files and versions the *origin* has
	pingURL            string             // absolute URL which we'll ping (e.g. http://example.com/static) every validity period expecting to receive latest "turbocharger" header
	pingCheckOnce      sync.Once          // to make sure ping check is done only once
	validUntil         <-chan struct{}    // obtained from ctx.Done(). closed when this *discoveredSubtree* should be considered stale.
}

func NewMiddleware(origin http.Handler, manifestHandler *ManifestHandler, logger *log.Logger) *turbochargerMiddleware {
	return &turbochargerMiddleware{
		origin:          origin,          // minimize requests to this
		manifestHandler: manifestHandler, // by using this

		logl: logex.Levels(logger),
	}
}

var _ http.Handler = (*turbochargerMiddleware)(nil)

func (t *turbochargerMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// non-panicing type assertion to get nil *discoveredSubtree (Load() can return untyped nil)
	// when initial discovery has not been done
	discovered, _ := t.discovered.Load().(*discoveredSubtree)

	if discovered != nil && strings.HasPrefix(r.URL.Path, discovered.subtreeVersion.Prefix) { // request can be turbocharged
		t.validityCheckMaybeTriggerPing(discovered, r)

		discovered.originTurbocharged.ServeHTTP(w, r)
	} else {
		t.origin.ServeHTTP(w, r)

		// we aren't expected to receive these after the first autodiscovery has completed.
		// (we won't be making any more requests to origin's subtree which has these)
		t.checkForTurbochargerAdvertisement(w.Header().Get(turbochargerAdvertisementHeaderKey), r)
	}
}

// if origin hints us that it has turbocharged tree available, we'll
func (t *turbochargerMiddleware) checkForTurbochargerAdvertisement(tcHeader string, r *http.Request) {
	if tcHeader == "" {
		return
	}

	// TODO: check that we didn't get "turbocharger" header for non-turbocharged prefix?

	subtree, err := parseTCHeader(tcHeader)
	if err != nil {
		t.logl.Error.Printf("checkForTurbochargerAdvertisement: parseTCHeader: %v", err)
		return
	}

	// only (process lifetime-)early races should lead to situations where this function finds multiple
	// advertisements. normally after the first discovery we're not hitting any URLs from origin that
	// contain the advertisement (except from the ping feature)
	if existing, found := t.discovered.Load().(*discoveredSubtree); found {
		if existing.subtreeVersion.Equal(*subtree) {
			t.logl.Debug.Println("pre-attach race detected (not dangerous)")
		} else { // shouldn't happen
			t.detachTurbocharger(fmt.Errorf("got multiple conflicting advertisements in pre-attach state: %s vs. %s; detaching turbocharger",
				existing.subtreeVersion.HeaderValue(),
				subtree.HeaderValue()))
		}
	} else {
		discovered := t.attachDiscoveredSubtree(*subtree, r)

		t.logl.Info.Printf("attached turbocharger: %s (pingURL=%s)", tcHeader, discovered.pingURL)
	}
}

// we need to ping origin often to prevent serving outdated version
func (t *turbochargerMiddleware) validityCheckMaybeTriggerPing(discoveredStale *discoveredSubtree, r *http.Request) {
	// about *validUntil*: we could've used a time instant and compute validity by comparing with
	// time.Now() but I wager this is more effficient as it saves a "what's the current time" syscall
	// and now we're only relying on context.WithTimeout() being performant
	select {
	default: // not actually stale
		return
	case <-discoveredStale.validUntil: // validity expired (stale from now on)
		// only make the first request trigger the ping check, and intentionally don't make any
		// off-validity-period requests block until this completes as not to cause periodic latency spikes.
		discoveredStale.pingCheckOnce.Do(func() {
			go func() {
				if err := t.pingCheck(discoveredStale, r); err != nil {
					t.detachTurbocharger(err)
				}
			}()
		})
	}
}

// if this returns error, the caller is responsible for detaching existing turbocharger
func (t *turbochargerMiddleware) pingCheck(discoveredStale *discoveredSubtree, r *http.Request) error {
	ping, err := http.NewRequest(http.MethodHead, discoveredStale.pingURL, io.NopCloser(bytes.NewReader(nil)))
	if err != nil {
		return err
	}

	// FIXME: don't use "httptest" package
	response := httptest.NewRecorder()
	t.origin.ServeHTTP(response, ping)

	if response.Code != http.StatusOK {
		return fmt.Errorf("ping request failed: %d", response.Code)
	}

	tcHeader := response.Header().Get(turbochargerAdvertisementHeaderKey)
	if tcHeader == "" {
		// considered an error b/c of context: if we're pinging, it means turbocharger existed before.
		return errors.New("turbocharger header went missing")
	}

	subtree, err := parseTCHeader(tcHeader)
	if err != nil {
		return err
	}

	// always create new discovery result, even if advertisement does not change
	// (to push *validUntil* forward and reset *pingCheckOnce*)
	reloaded := t.attachDiscoveredSubtree(*subtree, r)

	if !discoveredStale.subtreeVersion.Equal(reloaded.subtreeVersion) {
		t.logl.Info.Printf(
			"turbocharger reload from '%s' -> '%s'",
			discoveredStale.subtreeVersion.HeaderValue(),
			reloaded.subtreeVersion.HeaderValue())
	}

	return nil
}

func (t *turbochargerMiddleware) attachDiscoveredSubtree(subtree turbochargeSubtree, r *http.Request) *discoveredSubtree {
	/*
		the accepted time window in which we can serve outdated files. when this timeout expires, we'll
		ping the origin for its turbocharger advertisement to see if we still have up-to-date
		manifest ID & prefix. We do this pinging lazily, i.e. if no request in 10 minutes then 0 pings to origin.

		Internet  ─────────────────► Loadbalancer ───────────────► Origin
		             1 000 req/s                     1 req/5 s
	*/
	// lint complains about context leak. our use is pretty exotic here, but this'll get cancelled anyway
	//nolint:govet
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)

	discovered := &discoveredSubtree{
		subtreeVersion: subtree,
		pingURL:        createPingURL(r, subtree),
		validUntil:     ctx.Done(),
		originTurbocharged: http.StripPrefix(subtree.Prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := t.manifestHandler.ServeHTTPFromManifest(subtree.ManifestID, w, r); err != nil {
				t.logl.Error.Println(err.Error())
			}
		})),
	}

	t.discovered.Store(discovered)

	return discovered
}

// detaching is important because if ping requests to e.g. /static stop advertising
// turbocharger, it probably means that it was deployed without a turbocharger annotation
// (and thus the new version's files are not guaranteed to exist)
func (t *turbochargerMiddleware) detachTurbocharger(err error) {
	t.discovered.Store((*discoveredSubtree)(nil))

	t.logl.Error.Printf("detached turbocharger: %v", err)
}

// we maybe got request for /static/main.js. when validity period expires, we should query
// /static instead for the up-to-date turbocharger header (we can't assume that main.js exists across deploys)
func createPingURL(r *http.Request, subtree turbochargeSubtree) string {
	return (&url.URL{
		Scheme: func() string {
			if r.TLS != nil {
				return "https"
			} else {
				return "http"
			}
		}(),
		Host: r.Host, // not present in the URL
		Path: subtree.Prefix,
	}).String()
}

type turbochargeSubtree struct {
	Prefix     string // usually something like /static
	ManifestID ObjectID
}

func (t *turbochargeSubtree) Equal(other turbochargeSubtree) bool {
	return t.Prefix == other.Prefix && bytes.Equal(t.ManifestID[:], other.ManifestID[:])
}

func (t *turbochargeSubtree) HeaderValue() string {
	return fmt.Sprintf("%s %s", t.Prefix, t.ManifestID.String())
}

func parseTCHeader(serialized string) (*turbochargeSubtree, error) {
	parts := strings.Split(serialized, " ")

	prefix := parts[0]
	manifestID, err := ObjectIDFromString(parts[1])
	if err != nil {
		return nil, err
	}

	return &turbochargeSubtree{
		Prefix:     prefix,
		ManifestID: *manifestID,
	}, nil
}
