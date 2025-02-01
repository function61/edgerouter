package dockerdiscovery

import (
	"errors"
	"fmt"
	"strings"

	"github.com/function61/edgerouter/pkg/erconfig"
)

// find annotations from here:
//
//	https://docs.traefik.io/v1.7/configuration/backends/docker/
func traefikAnnotationsToApp(service Service) (*erconfig.Application, error) {
	// we used to have explicit check for label traefik.enable=true, but that was strictly
	// for Traefik itself so it doesn't expose everything by default (= security concern).
	// now that we've moved to Edgerouter, presence of this is enough for opt-in
	frontendRule := service.Labels["traefik.frontend.rule"]
	if frontendRule == "" {
		return nil, nil
	}

	scheme := "http"
	if proto, specified := service.Labels["traefik.protocol"]; specified {
		if proto != "http" && proto != "https" {
			return nil, fmt.Errorf("unsupported protocol: %s", proto)
		}

		scheme = proto
	}

	insecureSkipVerify, err := func() (bool, error) {
		// doesn't actually seem to exist in Traefik:
		//     https://github.com/containous/traefik/issues/2367
		if insecureSkipVerifyString, has := service.Labels["traefik.backend.tls.insecureSkipVerify"]; has {
			if insecureSkipVerifyString != "true" {
				return false, fmt.Errorf("unsupported value for insecureSkipVerify: %s", insecureSkipVerifyString)
			}

			if scheme != "https" {
				return false, errors.New("insecureSkipVerify specified but not using https")
			}

			return true, nil
		} else {
			return false, nil
		}
	}()
	if err != nil {
		return nil, err
	}

	// also doesn't exist in Traefik
	tlsServerName := service.Labels["traefik.backend.tls.serverName"]

	port := service.Labels["traefik.port"]
	if port == "" {
		if scheme == "http" {
			port = "80"
		} else if scheme == "https" {
			port = "443"
		}
	}

	frontend, err := func() (erconfig.Frontend, error) {
		parsed, err := parseTraefikFrontendRule(frontendRule)
		if err != nil {
			return erconfig.Frontend{}, err
		}

		opts := []erconfig.FrontendOpt{}
		if parsed.PathPrefix != "" {
			opts = append(opts, erconfig.PathPrefix(parsed.PathPrefix))
		}

		switch {
		case parsed.Host != "":
			return erconfig.SimpleHostnameFrontend(parsed.Host, opts...), nil
		case parsed.HostRegexp != "":
			return erconfig.RegexpHostnameFrontend(parsed.HostRegexp, opts...), nil
		default:
			return erconfig.Frontend{}, fmt.Errorf("unsupported frontend rule: %s", frontendRule)
		}
	}()
	if err != nil {
		return nil, err
	}

	addrs := []string{}

	for _, instance := range service.Instances {
		addrs = append(addrs, scheme+"://"+instance.IPv4+":"+port)
	}

	if len(addrs) == 0 {
		return nil, nil
	}

	tlsConfig := &erconfig.TlsConfig{
		InsecureSkipVerify: insecureSkipVerify,
		ServerName:         tlsServerName,
	}

	backend := erconfig.ReverseProxyBackend(
		addrs,
		tlsConfig.SelfOrNilIfNoMeaningfulContent(),
		true)

	// maybe wrap in auth backend
	backendAuthorized, err := func() (erconfig.Backend, error) {
		switch service.Labels["edgerouter.auth"] {
		case "public":
			// we require explicit opt-in to this for security, so missing keys don't accidentally expose sensitive endpoints
			return backend, nil // no wrapping
		case "bearer_token":
			bearerToken := service.Labels["edgerouter.auth_bearer_token"]
			if bearerToken == "" {
				return erconfig.Backend{}, errors.New("empty bearer token not supported")
			}

			return erconfig.AuthV0Backend(bearerToken, backend), nil
		case "sso":
			tenant := service.Labels["edgerouter.auth_sso.tenant"]
			if tenant == "" {
				return erconfig.Backend{}, errors.New("edgerouter.auth_sso.tenant empty")
			}

			// looks like t-2/monitoring_prometheus
			audience := fmt.Sprintf("%s/%s", tenant, service.Name)

			// is not a security issue if empty (nobody gets through then)
			users := strings.Split(service.Labels["edgerouter.auth_sso.users"], ",")

			return erconfig.AuthSsoBackend("", users, audience, backend), nil
		default:
			return erconfig.Backend{}, fmt.Errorf("unsupported auth mode: %s", service.Labels["edgerouter.auth"])
		}
	}()
	if err != nil {
		return nil, err
	}

	app := erconfig.SimpleApplication(
		service.Name,
		frontend,
		backendAuthorized)

	return &app, nil
}

type traefikFrontendRule struct {
	Host       string `json:",omitempty"` // `Host:...` rule
	PathPrefix string `json:",omitempty"` // `PathPrefix:...` rule
	HostRegexp string `json:",omitempty"` // `HostRegexp:...` rule
}

// https://doc.traefik.io/traefik/v1.7/user-guide/examples/#using-labels-in-docker-composeyml
func parseTraefikFrontendRule(rule string) (traefikFrontendRule, error) {
	parsed := traefikFrontendRule{}

	// `Host:example.com;PathPrefix:/admin/`
	// => `["Host:example.com", "PathPrefix:/admin/"]`
	subRules := strings.Split(rule, ";")

	for _, subRule := range subRules {
		switch {
		case strings.HasPrefix(subRule, "Host:"):
			parsed.Host = subRule[len("Host:"):]
		case strings.HasPrefix(subRule, "PathPrefix:"):
			parsed.PathPrefix = subRule[len("PathPrefix:"):]
		case strings.HasPrefix(subRule, "HostRegexp:"):
			parsed.HostRegexp = subRule[len("HostRegexp:"):]
		default:
			return parsed, fmt.Errorf("unsupported rule: '%s'", subRule)
		}
	}

	return parsed, nil
}
