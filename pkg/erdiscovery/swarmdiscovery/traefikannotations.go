package swarmdiscovery

import (
	"errors"
	"fmt"
	"strings"

	"github.com/function61/edgerouter/pkg/erconfig"
)

// find annotations from here:
//     https://docs.traefik.io/v1.7/configuration/backends/docker/
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
		switch {
		case strings.HasPrefix(frontendRule, "Host:"):
			return erconfig.SimpleHostnameFrontend(frontendRule[len("Host:"):], "/", false), nil
		case strings.HasPrefix(frontendRule, "HostRegexp:"):
			return erconfig.RegexpHostnameFrontend(frontendRule[len("HostRegexp:"):], "/"), nil
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

	backend := erconfig.ReverseProxyBackend(addrs, tlsConfig.SelfOrNilIfNoMeaningfulContent(), true)

	// doesn't exist in Traefik
	bearerToken, found := service.Labels["traefik.backend.auth_bearer_token"]
	if found {
		if bearerToken == "" {
			return nil, errors.New("empty bearer token not supported")
		}

		// wrap in auth backend
		backend = erconfig.AuthV0Backend(bearerToken, backend)
	}

	app := erconfig.SimpleApplication(
		service.Name,
		frontend,
		backend)

	return &app, nil
}
