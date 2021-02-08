package erserver

// IP-based filtering (access control). I used to think it is legacy enterprise BS and doesn't have any
// place in a modern stack, but once Tailscale made them "identities", I guess there is some value left.

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/function61/gokit/fileexists"
	"github.com/function61/gokit/hcl2json"
	"github.com/function61/gokit/jsonfile"
	"github.com/function61/gokit/sliceutil"
	"inet.af/netaddr"
)

const (
	ipRulesFile = "/etc/edgerouter/ip-rules.hcl" // temporary solution - these will have to live in EventHorizon
)

type ipRule struct {
	ipPrefix      netaddr.IPPrefix
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

	ipAndPort, err := netaddr.ParseIPPort(ipAndPortStr)
	if err != nil {
		return false, "invalid IP: " + err.Error()
	}

	// the port is not used for ACL (it's remote port anyway which is meaningless)
	return ipAllowedInternal(ipAndPort.IP, appToAccess, rules)
}

// do not use directly
func ipAllowedInternal(ip netaddr.IP, appToAccess string, rules []ipRule) (bool, string) {
	if matchingRule := ruleForIp(ip, rules); matchingRule != nil {
		if matchingRule.AllowsApp(appToAccess) {
			return true, ""
		} else {
			return false, fmt.Sprintf("your IP (%s) is not allowed (explicit deny)", ip.String())
		}
	}

	// no matching rule found => implicit deny
	return false, fmt.Sprintf("your IP (%s) is not allowed (implicit deny)", ip.String())
}

func ruleForIp(ip netaddr.IP, rules []ipRule) *ipRule {
	for _, rule := range rules {
		if rule.ipPrefix.Contains(ip) {
			return &rule
		}
	}

	return nil
}

// factories

// funky signature to make sure we get at least one
func allowOnlyApps(prefix netaddr.IPPrefix, app1 string, rest ...string) ipRule {
	return ipRule{prefix, append([]string{app1}, rest...)}
}

func allowAllApps(prefix netaddr.IPPrefix) ipRule {
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

func loadIpRules() ([]ipRule, error) {
	exists, err := fileexists.Exists(ipRulesFile)
	if err != nil {
		return nil, err
	}

	if !exists { // not an error => we just don't have any rules.. pun intended :)
		return nil, nil
	}

	f, err := os.Open(ipRulesFile)
	if err != nil {
		return nil, err
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
		rules = append(rules, allowAllApps(netaddr.MustParseIPPrefix(allowAllItem.Prefix)))
	}

	for _, allowSpecified := range conf.AllowOnlyApps {
		rules = append(rules, allowOnlyApps(netaddr.MustParseIPPrefix(allowSpecified.Prefix), allowSpecified.Apps[0], allowSpecified.Apps[1:]...))
	}

	if len(rules) == 0 {
		return nil, errors.New("empty IP rules file") // would be dangerous to accept
	}

	return rules, nil
}

func unmarhsalHcl(content io.Reader, data interface{}) error {
	// transform to JSON first, because we have better tools to unmarshal that
	asJson, err := hcl2json.Convert(content)
	if err != nil {
		return err
	}

	return jsonfile.Unmarshal(asJson, data, true)
}
