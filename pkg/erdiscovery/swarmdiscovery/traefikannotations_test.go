package swarmdiscovery

import (
	"encoding/json"
	"testing"

	"github.com/function61/gokit/assert"
)

type labels map[string]string

func TestTraefikAnnotationsToApp(t *testing.T) {
	ip101 := []ServiceInstance{{IPv4: "192.168.1.101"}}
	ip101And102 := []ServiceInstance{{IPv4: "192.168.1.101"}, {IPv4: "192.168.1.102"}}

	type testCase struct {
		input          Service
		expectedOutput string
	}

	mkTestCase := func(name string, ips []ServiceInstance, lab labels, expectedOutput string) testCase {
		return testCase{
			Service{
				Name:      name,
				Labels:    lab,
				Instances: ips,
			},
			expectedOutput,
		}
	}

	tcs := []testCase{
		mkTestCase("simpleHost", ip101And102, labels{
			"traefik.frontend.rule": "Host:www.example.com",
		}, `{
  "id": "simpleHost",
  "frontends": [
    {
      "kind": "hostname",
      "hostname": "www.example.com",
      "path_prefix": "/"
    }
  ],
  "backend": {
    "kind": "reverse_proxy",
    "reverse_proxy_opts": {
      "origins": [
        "http://192.168.1.101:80",
        "http://192.168.1.102:80"
      ],
      "pass_host_header": true
    }
  }
}`),
		mkTestCase("simpleHostHttps", ip101, labels{
			"traefik.frontend.rule": "Host:www.example.com",
			"traefik.protocol":      "https",
		}, `{
  "id": "simpleHostHttps",
  "frontends": [
    {
      "kind": "hostname",
      "hostname": "www.example.com",
      "path_prefix": "/"
    }
  ],
  "backend": {
    "kind": "reverse_proxy",
    "reverse_proxy_opts": {
      "origins": [
        "https://192.168.1.101:443"
      ],
      "pass_host_header": true
    }
  }
}`),
		mkTestCase("simpleHostHttpsInsecureSkipVerify", ip101, labels{
			"traefik.frontend.rule":                  "Host:www.example.com",
			"traefik.protocol":                       "https",
			"traefik.backend.tls.insecureSkipVerify": "true",
		}, `{
  "id": "simpleHostHttpsInsecureSkipVerify",
  "frontends": [
    {
      "kind": "hostname",
      "hostname": "www.example.com",
      "path_prefix": "/"
    }
  ],
  "backend": {
    "kind": "reverse_proxy",
    "reverse_proxy_opts": {
      "origins": [
        "https://192.168.1.101:443"
      ],
      "tls_config": {
        "insecure_skip_verify": true
      },
      "pass_host_header": true
    }
  }
}`),
		mkTestCase("regexpTlsWithPortAndServerName", ip101, labels{
			"traefik.frontend.rule":          "HostRegexp:traefik.{[^.]+}.example.com",
			"traefik.protocol":               "https",
			"traefik.port":                   "4486",
			"traefik.backend.tls.serverName": "www.example.com",
		}, `{
  "id": "regexpTlsWithPortAndServerName",
  "frontends": [
    {
      "kind": "hostname_regexp",
      "hostname_regexp": "traefik.{[^.]+}.example.com",
      "path_prefix": "/"
    }
  ],
  "backend": {
    "kind": "reverse_proxy",
    "reverse_proxy_opts": {
      "origins": [
        "https://192.168.1.101:4486"
      ],
      "tls_config": {
        "server_name": "www.example.com"
      },
      "pass_host_header": true
    }
  }
}`),
		mkTestCase("authorized", ip101, labels{
			"traefik.frontend.rule":             "Host:example.com",
			"traefik.backend.auth_bearer_token": "Hunter2",
		}, `{
  "id": "authorized",
  "frontends": [
    {
      "kind": "hostname",
      "hostname": "example.com",
      "path_prefix": "/"
    }
  ],
  "backend": {
    "kind": "auth_v0",
    "auth_v0_opts": {
      "bearer_token": "Hunter2",
      "authorized_backend": {
        "kind": "reverse_proxy",
        "reverse_proxy_opts": {
          "origins": [
            "http://192.168.1.101:80"
          ],
          "pass_host_header": true
        }
      }
    }
  }
}`),
	}

	for _, tc := range tcs {
		tc := tc // pin

		t.Run(tc.input.Name, func(t *testing.T) {
			app, err := traefikAnnotationsToApp(tc.input)
			assert.Ok(t, err)

			appJson, err := json.MarshalIndent(app, "", "  ")
			assert.Ok(t, err)

			assert.EqualString(t, string(appJson), tc.expectedOutput)
		})
	}
}
