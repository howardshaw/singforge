package render

import (
	"net"
	"slices"
	"sort"
	"strings"

	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/routing"
)

// Exact public service hosts only. A shared provider suffix can also host AI APIs.
var domesticHosts = []string{
	"www.12306.cn", "kyfw.12306.cn", "www.gov.cn", "www.jd.com",
	"item.jd.com", "www.taobao.com", "www.bilibili.com", "www.qq.com", "localhost.ptlogin2.qq.com",
}
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
	direct := via(bootstrap, "DIRECT")
	remote := via(bootstrap, "Main")
	ai := via(bootstrap, "AI")
	aiCN := []string{"https://dns.alidns.com/dns-query#DIRECT", "https://doh.pub/dns-query#DIRECT"}
	if s.DNSTransport == "tls" {
		aiCN = []string{"tls://dns.alidns.com:853#DIRECT", "tls://dot.pub:853#DIRECT"}
	}
	policies := M{}
	for _, domain := range []string{"+.localhost", "+.home.arpa", "+.local", "+.lan"} {
		policies[domain] = "rcode://name_error"
		if domain != "+.localhost" && s.DNS == "balanced" {
			policies[domain] = "system"
		}
	}
	addPolicy := func(r routing.Rule) {
		group := ai
		if r.Group == "AI-CN" {
			if s.MihomoPolicy == "split" {
				group = aiCN
			}
		} else if r.Group != "AI" {
			return
		}
		switch r.Kind {
		case "DOMAIN":
			policies[r.Value] = group
		case "DOMAIN-SUFFIX":
			policies["+."+r.Value] = group
		}
	}
	for _, r := range smartServices {
		addPolicy(r)
	}
	for _, r := range s.Rules.Rules {
		addPolicy(r)
	}
	for _, r := range s.Overrides {
		addPolicy(r)
	}
	if s.MihomoPolicy == "split" && s.Routing == "smart" {
		cn := []string{"https://dns.alidns.com/dns-query#DIRECT", "https://doh.pub/dns-query#DIRECT"}
		if s.DNSTransport == "tls" {
			cn = []string{"tls://dns.alidns.com:853#DIRECT", "tls://dot.pub:853#DIRECT"}
		}
		for _, h := range domesticHosts {
			// Keep AI classifications protected even in the presence of a user override.
			sensitive := slices.ContainsFunc(rules, func(r routing.Rule) bool { return (r.Group == "AI" || r.Group == "AI-CN") && r.Match(h) })
			if sensitive {
				continue
			}
			for _, r := range rules {
				if r.Match(h) {
					if r.Group == "DIRECT" {
						policies[h] = cn
					}
					break
				}
			}
		}
	}
	return M{"enable": true, "listen": "127.0.0.1:1053", "ipv6": ipv6,
		"enhanced-mode": "fake-ip", "fake-ip-range": "198.18.0.1/16", "fake-ip-filter": realIPHosts,
		"use-hosts": true, "use-system-hosts": s.DNS == "balanced" && s.MihomoPolicy != "split", "respect-rules": true,
		"default-nameserver": bootstrap, "proxy-server-nameserver": direct,
		"direct-nameserver": direct, "direct-nameserver-follow-policy": true,
		"nameserver": remote, "nameserver-policy": policies,
	}
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
