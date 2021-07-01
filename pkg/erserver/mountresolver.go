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

type frontendMatchers struct {
	Hostname       map[string]MountList // hostname equality
	hostnameRegexp []hostnameRegexp
	Apps           []erconfig.Application
	timestamp      time.Time
}

func newFrontendMatchers(apps []erconfig.Application, timestamp time.Time) *frontendMatchers {
	return &frontendMatchers{
		Hostname:       map[string]MountList{},
		hostnameRegexp: []hostnameRegexp{},
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
			pathMatcher := Mount{
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

				mountList = append(mountList, pathMatcher)

				// order mount list based on the path length, so longer paths are considered
				// before root
				sort.Slice(mountList, func(i, j int) bool { return len(mountList[i].prefix) > len(mountList[j].prefix) })

				fem.Hostname[frontend.Hostname] = mountList
			case erconfig.FrontendKindHostnameRegexp:
				re, err := hostnameRegexpSyntaxToRegexp(frontend.HostnameRegexp)
				if err != nil {
					return nil, err
				}

				// TODO: support for same regexp multiple times, with different paths

				fem.hostnameRegexp = append(fem.hostnameRegexp, hostnameRegexp{
					Regexp: re,
					Mounts: MountList{pathMatcher},
				})
			default:
				return nil, errors.New("unsupported frontend kind")
			}
		}
	}

	return fem, nil
}

func mountListForHostname(hostname string, path string, matchers *frontendMatchers) MountList {
	// try first with exact hostname
	paths, hostnameFound := matchers.Hostname[hostname]
	if hostnameFound {
		return paths
	}

	// then, try regexp-based hostnames
	for _, re := range matchers.hostnameRegexp {
		if re.Regexp.MatchString(hostname) {
			return re.Mounts
		}
	}

	// TODO: third option, dynamic tenant lookup

	return nil
}

func resolveMount(hostname string, path string, matchers *frontendMatchers) *Mount {
	mounts := mountListForHostname(hostname, path, matchers)
	if mounts == nil {
		return nil
	}

	for _, mount := range mounts {
		if mount.prefix == "/" { // always matches
			return &mount
		}

		// normalize "/foo/" => "/foo"
		prefix := strings.TrimRight(mount.prefix, "/")

		// prefix="/foo" should match "/foo", "/foo/.*" but not "/foobar"
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return &mount
		}
	}

	return nil
}
