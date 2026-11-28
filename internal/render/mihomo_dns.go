package render

import (
	"fmt"
	"net"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/routing"
)

var realIPHosts = []string{"localhost", "+.localhost", "+.lan", "+.local", "+.home.arpa", "captive.apple.com", "+.msftconnecttest.com", "+.msftncsi.com", "localhost.ptlogin2.qq.com"}

func encryptedURL(scheme, host string) string {
	if scheme == "tls" {
		return "tls://" + net.JoinHostPort(host, "853")
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "https://" + host + "/dns-query"
}
func via(resolvers []string, group string) []string {
	out := make([]string, 0, len(resolvers))
	for _, r := range resolvers {
		out = append(out, r+"#"+group)
	}
	return out
}

// On IPv6-only endpoint sets an unspecified resolver family must also be usable.
func mihomoFamilies(s model.State, nodes []model.Node) (bool, string) {
	family := s.DNSFamily
	if family == "" && len(nodes) > 0 && !slices.ContainsFunc(nodes, func(n model.Node) bool { ip := net.ParseIP(n.Host); return ip == nil || ip.To4() != nil }) {
		family = "ipv6"
	}
	if s.MihomoIPv6 != "" {
		return s.MihomoIPv6 == "on", family
	}
	return family == "ipv6", family
}
func mihomoDNS(s model.State, rules []routing.Rule, ipv6 bool) M {
	var bootstrap []string
	for _, provider := range []string{s.Resolver, "cloudflare", "google", "quad9"} {
		selected := s
		selected.Resolver = provider
		addr, scheme := resolver(selected)
		u := encryptedURL(scheme, addr)
		if !slices.Contains(bootstrap, u) {
			bootstrap = append(bootstrap, u)
		}
	}
	nodeDNS := via(bootstrap, "DIRECT")
	remote := via(bootstrap, "Main")
	ai := via(bootstrap, "AI")
	dnsPod := "https://doh.pub/dns-query#DIRECT"
	if s.DNSTransport == "tls" {
		dnsPod = "tls://dot.pub:853#DIRECT"
	}
	ali := aliDNSResolvers(s.DNSFamily, s.DNSTransport)
	domestic := append(via(ali, "DIRECT"), dnsPod)
	domesticAI := domestic
	domesticFirst := s.MihomoPolicy == "domestic-first"
	useDomesticDNS := s.MihomoPolicy == "split" || domesticFirst
	if useDomesticDNS {
		bootstrap = ali
	}
	if domesticFirst {
		domesticAI = via(ali, "AI-CN")
	}
	policies := M{}
	for _, domain := range []string{"+.localhost", "+.home.arpa", "+.local", "+.lan"} {
		policies[domain] = "rcode://name_error"
		if domain != "+.localhost" && s.DNS == "balanced" {
			policies[domain] = "system"
		}
	}
	// DNS uses the most specific key, while traffic uses the first matching rule.
	// Resolve each representable key against the ordered policy before exporting it.
	ordered := rules
	if s.Routing != "smart" {
		// Other presets also protect known AI queries even without service routing.
		ordered = append(slices.Clone(rules), smartServices...)
		for _, r := range s.Rules.Rules {
			if r.Group == "AI" || r.Group == "AI-CN" {
				ordered = append(ordered, r)
			}
		}
	}
	for i, candidate := range ordered {
		if candidate.Kind != "DOMAIN" && candidate.Kind != "DOMAIN-SUFFIX" {
			continue
		}
		key := candidate.Value
		if candidate.Kind == "DOMAIN-SUFFIX" {
			key = "+." + key
		}
		if _, exists := policies[key]; exists {
			continue
		}
		if i >= len(s.Overrides) && candidate.Group != "AI" && candidate.Group != "AI-CN" {
			continue
		}
		group := candidate.Group
		safeForDomestic := true
		for _, r := range ordered[:i] {
			if candidate.Kind == "DOMAIN-SUFFIX" && r.Kind == "DOMAIN" {
				continue // An exact rule cannot classify every descendant.
			}
			if r.Match(candidate.Value) {
				group = r.Group
				break
			}
			if candidate.Kind == "DOMAIN-SUFFIX" && r.Kind == "DOMAIN-KEYWORD" && r.Group != "AI-CN" {
				// A higher-priority keyword may match only some descendants.
				safeForDomestic = false
			}
		}
		switch group {
		case "AI":
			policies[key] = ai
		case "AI-CN":
			policies[key] = ai
			// A traffic override, downloaded category or shared CDN is not
			// permission to disclose additional names to domestic resolvers.
			if useDomesticDNS && safeForDomestic && slices.ContainsFunc(smartServices, func(r routing.Rule) bool {
				return r.Group == "AI-CN" && r.Match(candidate.Value)
			}) {
				policies[key] = domesticAI
			}
		default:
			// Explicit exceptions also outrank the DNS GeoSite category.
			policies[key] = remote
		}
	}
	if s.Routing == "smart" {
		policies["geosite:"+geoAI] = ai
		if useDomesticDNS {
			policies["geosite:cn"] = via(ali, "DIRECT")
		}
	}
	return M{"enable": true, "listen": "127.0.0.1:1053", "ipv6": ipv6,
		"enhanced-mode": "fake-ip", "fake-ip-range": "198.18.0.1/16", "fake-ip-filter": realIPHosts,
		"use-hosts": true, "use-system-hosts": s.DNS == "balanced" && s.MihomoPolicy != "split" && !domesticFirst, "respect-rules": !useDomesticDNS,
		"default-nameserver": bootstrap, "proxy-server-nameserver": nodeDNS,
		"direct-nameserver": remote, "direct-nameserver-follow-policy": true,
		"nameserver": remote, "nameserver-policy": policies,
	}
}

// A keyword can match names in the CN set that are unknown at export time.
// Inline domain matchers preserve their DNS exceptions without expanding CN.
func addKeywordDNS(cfg, dns M, rules []routing.Rule) {
	policies := dns["nameserver-policy"].(M)
	if _, ok := policies["geosite:cn"]; !ok {
		return
	}
	providers := M{}
	for _, r := range rules {
		if r.Kind != "DOMAIN-KEYWORD" {
			continue
		}
		name := fmt.Sprintf("dns-keyword-%08d", len(providers))
		providers[name] = M{"type": "inline", "behavior": "classical", "payload": []string{r.Kind + "," + r.Value}}
		upstreams := dns["nameserver"]
		if r.Group == "AI" || r.Group == "AI-CN" {
			upstreams = policies["geosite:"+geoAI]
		}
		policies["rule-set:"+name] = upstreams
	}
	if len(providers) > 0 {
		cfg["rule-providers"] = providers
	}
}

// Mihomo checks policy entries in YAML order. A GeoSite match ends the
// search, including for an exact exception whose name sorts after "geosite:".
type mihomoDNSPolicies M

func (policies mihomoDNSPolicies) MarshalYAML() (any, error) {
	var node yaml.Node
	if err := node.Encode(M(policies)); err != nil {
		return nil, err
	}
	var domains, keywords, categories []*yaml.Node
	for i := 0; i < len(node.Content); i += 2 {
		pair := node.Content[i : i+2]
		if strings.HasPrefix(pair[0].Value, "geosite:") {
			categories = append(categories, pair...)
		} else if strings.HasPrefix(pair[0].Value, "rule-set:") {
			keywords = append(keywords, pair...)
		} else {
			domains = append(domains, pair...)
		}
	}
	// category-ai-!cn already sorts before cn within the category entries.
	node.Content = append(append(domains, keywords...), categories...)
	return &node, nil
}

func orderedMihomoTags(nodes []model.Node) []string {
	sorted := slices.Clone(nodes)
	rank := func(n model.Node) int {
		switch {
		case n.Mode == "direct" && n.Spec.ID == "reality":
			return 0
		case n.Mode == "direct" && n.Spec.ID == "hysteria2":
			return 1
		case n.Mode == "tunnel" && n.Spec.ID == "vless-ws":
			return 2
		case n.Mode == "tunnel" && n.Spec.ID == "trojan-ws":
			return 3
		default:
			return 4
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return rank(sorted[i]) < rank(sorted[j]) })
	tags := make([]string, 0, len(nodes))
	for _, n := range sorted {
		tags = append(tags, n.ID)
	}
	return tags
}

func aliDNSResolvers(family, transport string) []string {
	addresses := []string{"223.5.5.5", "223.6.6.6"}
	if family == "ipv6" {
		addresses = []string{"2400:3200::1", "2400:3200:baba::1"}
	}
	var resolvers []string
	for _, addr := range addresses {
		resolvers = append(resolvers, encryptedURL(transport, addr))
	}
	return resolvers
}

// Restrict the opt-in resolver to the actual dial hosts of this export.
func addNodeDNS(dns M, hosts []string, family, transport string) {
	resolvers := via(aliDNSResolvers(family, transport), "DIRECT")
	policy := M{}
	for _, h := range hosts {
		if net.ParseIP(h) == nil {
			policy[h] = resolvers
		}
	}
	if len(policy) > 0 {
		dns["proxy-server-nameserver-policy"] = policy
	}
}
