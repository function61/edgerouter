package erserver

// IP-based filtering (access control). I used to think it is legacy enterprise BS and doesn't have any
// place in a modern stack, but once Tailscale made them "identities", I guess there is some value left.

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"

	"github.com/function61/gokit/hcl2json"
	"github.com/function61/gokit/jsonfile"
	"github.com/function61/gokit/sliceutil"
)

type ipRule struct {
	ipPrefix      netip.Prefix
	allowedAppIds []string // if empty, means all apps are allowed
}

func (i ipRule) AllowsApp(appToAccess string) bool {
	if len(i.allowedAppIds) == 0 { // allow all
		return true
	}

	return sliceutil.ContainsString(i.allowedAppIds, appToAccess)
}

func ipAllowed(ipAndPortStr string, appToAccess string, rules []ipRule) (bool, string) {
	if len(rules) == 0 { // no rules => no IP filtering in use
		return true, ""
	}

	ipAndPort, err := netip.ParseAddrPort(ipAndPortStr)
	if err != nil {
		return false, "invalid IP: " + err.Error()
	}

	// the port is not used for ACL (it's remote port anyway which is meaningless)
	return ipAllowedInternal(ipAndPort.Addr(), appToAccess, rules)
}

// do not use directly
func ipAllowedInternal(ip netip.Addr, appToAccess string, rules []ipRule) (bool, string) {
	if matchingRule := ruleForIP(ip, rules); matchingRule != nil {
		if matchingRule.AllowsApp(appToAccess) {
			return true, ""
		} else {
			return false, fmt.Sprintf("your IP (%s) is not allowed (explicit deny)", ip.String())
		}
	}

	// no matching rule found => implicit deny
	return false, fmt.Sprintf("your IP (%s) is not allowed (implicit deny)", ip.String())
}

func ruleForIP(ip netip.Addr, rules []ipRule) *ipRule {
	for _, rule := range rules {
		if rule.ipPrefix.Contains(ip) {
			return &rule
		}
	}

	return nil
}

// factories

// funky signature to make sure we get at least one app (0 apps by accident would be catastrophic)
func allowOnlyApps(prefix netip.Prefix, app1 string, appN ...string) ipRule {
	return ipRule{prefix, append([]string{app1}, appN...)}
}

func allowAllApps(prefix netip.Prefix) ipRule {
	return ipRule{prefix, nil}
}

// temporary rules file format. you can see example in tests

type ipRulesConfig struct {
	AllowAllApps []struct {
		Prefix string `json:"prefix"`
	} `json:"allow_all"`
	AllowOnlyApps []struct {
		Prefix string   `json:"prefix"`
		Apps   []string `json:"apps"`
	} `json:"allow_specified"`
}

func loadIPRules(ipRulesFile string) ([]ipRule, error) {
	f, err := os.Open(ipRulesFile)
	if err != nil {
		if os.IsNotExist(err) { // not an error => we just don't have any rules.. pun intended :)
			return nil, nil
		} else { // actual error
			return nil, err
		}
	}
	defer f.Close()

	return parseHclRules(f)
}

func parseHclRules(content io.Reader) ([]ipRule, error) {
	conf := &ipRulesConfig{}
	if err := unmarhsalHcl(content, conf); err != nil {
		return nil, err
	}

	rules := []ipRule{}

	for _, allowAllItem := range conf.AllowAllApps {
		rules = append(rules, allowAllApps(mustParsePrefix(allowAllItem.Prefix)))
	}

	for _, allowSpecified := range conf.AllowOnlyApps {
		rules = append(rules, allowOnlyApps(mustParsePrefix(allowSpecified.Prefix), allowSpecified.Apps[0], allowSpecified.Apps[1:]...))
	}

	if len(rules) == 0 {
		return nil, errors.New("empty IP rules file") // would be dangerous to accept
	}

	return rules, nil
}

func unmarhsalHcl(content io.Reader, data interface{}) error {
	// transform to JSON first, because we have better tools to unmarshal that
	asJSON, err := hcl2json.Convert(content)
	if err != nil {
		return err
	}

	return jsonfile.Unmarshal(asJSON, data, true)
}

func mustParsePrefix(rawPrefix string) netip.Prefix {
	prefix, err := netip.ParsePrefix(rawPrefix)
	if err != nil {
		panic(err)
	}

	return prefix
}
