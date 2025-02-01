package erserver

import (
	"fmt"
	"strings"
	"testing"

	"github.com/function61/gokit/assert"
	"inet.af/netaddr"
)

func TestNoRulesAllAllowed(t *testing.T) {
	allowed, _ := ipAllowed("1.2.3.4:1234", "anyapp", nil)
	assert.Assert(t, allowed)
}

func TestInvalidIP(t *testing.T) {
	invalidIp := "500.400.300.200.100:80"

	// without rules IP parsing is skipped
	allowed, errStr := ipAllowed(invalidIp, "anyapp", nil)
	assert.Assert(t, allowed)
	assert.Assert(t, errStr == "")

	allowed, errStr = ipAllowed(invalidIp, "anyapp", []ipRule{allowAllApps(netaddr.MustParseIPPrefix("0.0.0.0/0"))})
	assert.Assert(t, !allowed)
	assert.EqualString(t, errStr, `invalid IP: ParseIP("500.400.300.200.100"): IPv4 field has value >255`)
}

func TestIpFilter(t *testing.T) {
	rules, err := parseHclRules(strings.NewReader(`
allow_all { prefix = "127.0.0.0/24" } # this exact server
allow_all { prefix = "192.168.1.0/24" } # trusted VLAN
allow_all { prefix = "100.75.44.30/32" } # joonas work

allow_specified {
	prefix = "100.56.80.66/32" # joonas phone
	apps = ["test"]
}
`))
	assert.Ok(t, err)

	//nolint:gocritic // intentionally useless lambda, but useful as shorthand
	ip := func(ipStr string) netaddr.IP { // shorthand
		return netaddr.MustParseIP(ipStr)
	}

	assert.EqualString(t, ruleForIp(ip("192.168.1.18"), rules).ipPrefix.String(), "192.168.1.0/24")
	assert.EqualString(t, ruleForIp(ip("127.0.0.200"), rules).ipPrefix.String(), "127.0.0.0/24")
	assert.Assert(t, ruleForIp(ip("127.0.1.200"), rules) == nil)

	assert.EqualString(t, ruleForIp(ip("100.75.44.30"), rules).ipPrefix.String(), "100.75.44.30/32")
	assert.Assert(t, ruleForIp(ip("100.75.44.31"), rules) == nil)

	assert.EqualJson(t, ruleForIp(ip("100.56.80.66"), rules).allowedAppIds, `[
  "test"
]`)

	ipThatCanAccessTestApp := "100.56.80.66"
	ipThatCanAccessAnything := "100.75.44.30"

	for _, tc := range []struct {
		ip             string
		app            string
		expectedOutput string
	}{
		{
			ipThatCanAccessTestApp,
			"test",
			"allow",
		},
		{
			ipThatCanAccessTestApp,
			"foo", // .. but not this app
			"your IP (100.56.80.66) is not allowed (explicit deny)",
		},
		{
			ipThatCanAccessAnything,
			"test",
			"allow",
		},
		{
			ipThatCanAccessAnything,
			"foo",
			"allow",
		},
		{
			"100.75.43.30", // can access nothing, but /24 neighbor can access anything
			"test",
			"your IP (100.75.43.30) is not allowed (implicit deny)",
		},
		{
			"100.56.80.67", // can access nothing, but /32 neighbor can access test
			"test",
			"your IP (100.56.80.67) is not allowed (implicit deny)",
		},
	} {
		testcaseSubject := fmt.Sprintf("%s -> %s", tc.ip, tc.app) // for failures

		t.Run(testcaseSubject, func(t *testing.T) {
			allowed, errorStr := ipAllowed(tc.ip+":1234", tc.app, rules)

			output := func() string {
				if allowed {
					return "allow"
				} else {
					return errorStr
				}
			}()

			assert.EqualString(t, output, tc.expectedOutput)
		})
	}

}
