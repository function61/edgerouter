package erserver

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/function61/edgerouter/pkg/erconfig"
)

type hostnameRegexp struct {
	Regexp *regexp.Regexp
	Mounts MountList
}

type Mount struct {
	prefix            string
	stripPrefix       bool
	App               erconfig.Application
	backend           http.Handler
	allowInsecureHTTP bool
}

// these are ordered from longest to shortest
type MountList []Mount

// order mount list based on the path length, so longer paths are considered before root
func (m MountList) sortMountsFromLongestToShortest(i, j int) bool {
	return len(m[i].prefix) > len(m[j].prefix)
}

type frontendMatchers struct {
	Hostname       map[string]MountList // hostname equality
	hostnameRegexp []hostnameRegexp
	PathPrefix     MountList // global "all hostnames" path prefix rules like http://ANY_HOSTNAME/.well-known/acme-challenge/TOKEN
	Apps           []erconfig.Application
	timestamp      time.Time
}

func newFrontendMatchers(apps []erconfig.Application, timestamp time.Time) *frontendMatchers {
	return &frontendMatchers{
		Hostname:       map[string]MountList{},
		hostnameRegexp: []hostnameRegexp{},
		PathPrefix:     MountList{},
		Apps:           apps,
		timestamp:      timestamp,
	}
}

// transforms config (erconfig.Application) to concrerete instances (http.Handler) of backend for each app
func appConfigToHandlersAndMatchers(
	apps []erconfig.Application,
	currentConfig erconfig.CurrentConfigAccessor,
	timestamp time.Time,
) (*frontendMatchers, error) {
	fem := newFrontendMatchers(apps, timestamp)

	for _, app := range apps {
		backend, err := makeBackend(app.Id, app.Backend, currentConfig)
		if err != nil {
			return nil, fmt.Errorf("makeBackend: %s: %w", app.Id, err)
		}

		for _, frontend := range app.Frontends {
			mount := Mount{
				App:               app,
				backend:           backend,
				prefix:            frontend.PathPrefix,
				stripPrefix:       frontend.StripPathPrefix,
				allowInsecureHTTP: frontend.AllowInsecureHTTP,
			}

			switch frontend.Kind {
			case erconfig.FrontendKindHostname:
				if _, exists := fem.Hostname[frontend.Hostname]; !exists {
					fem.Hostname[frontend.Hostname] = MountList{}
				}

				mountList := fem.Hostname[frontend.Hostname]

				// TODO: detect duplicate (hostname, pathprefix) combos

				mountList = append(mountList, mount)

				sort.Slice(mountList, mountList.sortMountsFromLongestToShortest)

				fem.Hostname[frontend.Hostname] = mountList
			case erconfig.FrontendKindHostnameRegexp:
				re, err := hostnameRegexpSyntaxToRegexp(frontend.HostnameRegexp)
				if err != nil {
					return nil, err
				}

				// TODO: support for same regexp multiple times, with different paths

				fem.hostnameRegexp = append(fem.hostnameRegexp, hostnameRegexp{
					Regexp: re,
					Mounts: MountList{mount},
				})
			case erconfig.FrontendKindPathPrefix:
				fem.PathPrefix = append(fem.PathPrefix, mount)

				sort.Slice(fem.PathPrefix, fem.PathPrefix.sortMountsFromLongestToShortest)
			default:
				return nil, errors.New("unsupported frontend kind")
			}
		}
	}

	return fem, nil
}

func resolveMount(hostname string, path string, matchers *frontendMatchers) *Mount {
	pathMatches := func(mount *Mount) bool {
		if mount.prefix == "/" { // always matches
			return true
		}

		// normalize "/foo/" => "/foo"
		prefix := strings.TrimRight(mount.prefix, "/")

		// prefix="/foo" should match "/foo", "/foo/.*" but not "/foobar"
		matches := path == prefix || strings.HasPrefix(path, prefix+"/")

		return matches
	}

	// hostname-independent path-based mounts
	for _, mount := range matchers.PathPrefix {
		if pathMatches(&mount) {
			return &mount
		}
	}

	// try with exact hostname. this will probably be the most common case
	if hostnameMounts, hostnameFound := matchers.Hostname[hostname]; hostnameFound {
		for _, mount := range hostnameMounts {
			if pathMatches(&mount) {
				return &mount
			}
		}
	}

	// try regexp-based hostnames
	for _, hostnameRegexp := range matchers.hostnameRegexp {
		if !hostnameRegexp.Regexp.MatchString(hostname) {
			continue
		}

		for _, mount := range hostnameRegexp.Mounts {
			if pathMatches(&mount) {
				return &mount
			}
		}
	}

	return nil // will be a 404
}
