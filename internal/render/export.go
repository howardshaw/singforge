package render

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/howardshaw/singforge/internal/edge"
	"github.com/howardshaw/singforge/internal/model"
)

func Explain(s model.State, domain string) string {
	rules, end := policy(s)
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	for _, r := range rules {
		if r.Match(domain) {
			return r.Group + " (" + r.Kind + " " + r.Value + ")"
		}
	}
	return end + " (final)"
}

var addresses = map[string]string{"google": "8.8.8.8", "cloudflare": "1.1.1.1", "quad9": "9.9.9.9"}
var names = map[string]string{"google": "dns.google", "cloudflare": "cloudflare-dns.com", "quad9": "dns.quad9.net"}

func resolver(s model.State) (string, string) {
	addr := addresses[s.Resolver]
	if s.DNSFamily == "ipv6" {
		addr = map[string]string{"google": "2001:4860:4860::8888", "cloudflare": "2606:4700:4700::1111", "quad9": "2620:fe::fe"}[s.Resolver]
	}
	scheme := s.DNSTransport
	if scheme == "" {
		scheme = "https"
	}
	return addr, scheme
}
func singClient(s model.State, nodes []model.Node) ([]byte, error) {
	obs := []M{}
	tags := []string{}
	for _, n := range nodes {
		b := outbound(n)
		if net.ParseIP(n.Host) == nil {
			b["domain_resolver"] = "bootstrap"
		}
		if n.Spec.ID == "shadowtls" {
			b["tag"] = n.ID + "-outer"
			b["version"] = 3
			b["tls"] = M{"enabled": true, "server_name": n.Handshake}
			obs = append(obs, b)
			b = M{"type": "shadowsocks", "tag": n.ID, "method": method("ss"), "password": n.Auth.UUID, "detour": n.ID + "-outer"}
		}
		obs = append(obs, b)
		tags = append(tags, n.ID)
	}
	obs = append(obs, M{"type": "selector", "tag": "Main", "outbounds": tags})
	rules := []M{{"port": 53, "action": "hijack-dns"}}
	{
		for _, g := range []string{"AI", "AI-CN", "Streaming", "Communication"} {
			obs = append(obs, M{"type": "selector", "tag": g, "outbounds": append([]string{"Main"}, tags...)})
		}
	}
	obs = append(obs, M{"type": "direct", "tag": "DIRECT"})
	pol, end := policy(s)
	for i := 0; i < len(pol); {
		r := pol[i]
		j := i + 1
		vals := []string{r.Value}
		for j < len(pol) && pol[j].Group == r.Group && pol[j].Kind == r.Kind {
			vals = append(vals, pol[j].Value)
			j++
		}
		k := map[string]string{"DOMAIN-KEYWORD": "domain_keyword", "DOMAIN": "domain", "DOMAIN-SUFFIX": "domain_suffix", "IP-CIDR": "ip_cidr", "IP-CIDR6": "ip_cidr"}[r.Kind]
		v := M{k: vals, "action": "route", "outbound": r.Group}
		if r.Group == "REJECT" {
			delete(v, "outbound")
			v["action"] = "reject"
		}
		rules = append(rules, v)
		i = j
	}
	addr, scheme := resolver(s)
	servers := []M{{"type": scheme, "tag": "bootstrap", "server": addr, "tls": M{"enabled": true, "server_name": names[s.Resolver]}}, {"type": scheme, "tag": "remote", "server": addr, "tls": M{"enabled": true, "server_name": names[s.Resolver]}, "detour": "Main"}}
	if s.MihomoPolicy == "split" {
		servers = append(servers, M{"type": "https", "tag": "domestic-ai", "server": "dns.alidns.com", "server_port": 443, "path": "/dns-query", "tls": M{"enabled": true, "server_name": "dns.alidns.com"}, "domain_resolver": "bootstrap"})
	}
	dns := M{"servers": servers, "final": "remote", "rules": []M{{"domain_suffix": []string{"local", "lan", "home.arpa", "localhost"}, "action": "reject"}}}
	if s.MihomoPolicy == "split" {
		dns["rules"] = append(dns["rules"].([]M), M{"domain_suffix": []string{"deepseek.com", "moonshot.cn", "kimi.com", "qwen.ai", "tongyi.aliyun.com", "chatglm.cn", "bigmodel.cn", "doubao.com"}, "action": "route", "server": "domestic-ai"})
	}
	if s.DNS == "balanced" {
		dns["servers"] = append(servers, M{"type": "local", "tag": "local"})
		dns["rules"] = []M{{"domain_suffix": []string{"lan", "local", "home.arpa"}, "action": "route", "server": "local"}}
	}
	ins := []M{{"type": "mixed", "tag": "local-proxy", "listen": "127.0.0.1", "listen_port": 7890}}
	route := M{"default_domain_resolver": "remote", "rules": rules, "final": end}
	if s.Routing == "smart" && s.MihomoPolicy == "split" {
		route["rules"] = append(route["rules"].([]M), M{"domain_suffix": []string{"deepseek.com", "moonshot.cn", "kimi.com", "qwen.ai", "tongyi.aliyun.com", "chatglm.cn", "bigmodel.cn", "doubao.com"}, "action": "route", "outbound": "AI-CN"})
	}
	if s.Capture == "tun" {
		ins = append(ins, M{"type": "tun", "tag": "tun-in", "address": []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"}, "auto_route": true, "strict_route": true})
		route["auto_detect_interface"] = true
		dns["servers"] = append(dns["servers"].([]M), M{"type": "fakeip", "tag": "fake", "inet4_range": "198.18.0.0/15", "inet6_range": "2001:2::/48"})
		dr, _ := dns["rules"].([]M)
		dns["rules"] = append(dr, M{"inbound": "tun-in", "query_type": []string{"A", "AAAA"}, "action": "route", "server": "fake"})
	}
	config := M{"inbounds": ins, "outbounds": obs, "dns": dns, "route": route}
	if s.Routing == "smart" {
		config["http_clients"] = []M{{"tag": "geo-rules", "domain_resolver": "bootstrap"}}
		config["route"].(M)["default_http_client"] = "geo-rules"
		config["route"].(M)["rule_set"] = []M{
			{"type": "remote", "tag": "cn-domains", "format": "binary", "url": "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/sing/geo/geosite/cn.srs", "update_interval": "24h", "http_client": "geo-rules"},
			{"type": "remote", "tag": "cn-ips", "format": "binary", "url": "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/sing/geo/geoip/cn.srs", "update_interval": "24h", "http_client": "geo-rules"}}
		route["rules"] = append(route["rules"].([]M), M{"rule_set": []string{"cn-domains"}, "action": "route", "outbound": "DIRECT"}, M{"rule_set": []string{"cn-ips"}, "action": "route", "outbound": "DIRECT"})
		config["experimental"] = M{"cache_file": M{"enabled": true}}
	}
	return JSON(config)

}
func mihomo(n model.Node) M {
	a, p := n.Auth, n.Spec
	b := M{"name": n.ID, "type": p.Type, "server": n.Host, "port": n.Port}
	switch p.Type {
	case "shadowsocks":
		b["type"] = "ss"
		b["cipher"] = method(p.ID)
		b["password"] = a.Password
		b["udp"] = true
	case "vless", "vmess":
		b["uuid"] = a.UUID
		if p.Type == "vmess" {
			b["alterId"] = 0
			b["cipher"] = "auto"
		}
	case "tuic":
		b["uuid"] = a.UUID
		b["password"] = a.Password
		b["alpn"] = []string{"h3"}
	default:
		b["password"] = a.Password
	}
	if p.TLS || p.WS {
		field := "sni"
		if p.Type == "vmess" || p.Type == "vless" {
			field = "servername"
			b["tls"] = true
		}
		b[field] = n.SNI
		if p.TLS {
			der, _ := pem.Decode([]byte(n.Certificate))
			if der != nil {
				fp := sha256.Sum256(der.Bytes)
				b["fingerprint"] = hex.EncodeToString(fp[:])
			}
		}
	}
	if p.WS {
		b["network"] = "ws"
		b["ws-opts"] = M{"path": a.Path, "headers": M{"Host": n.Host}}
	}
	if strings.HasPrefix(p.ID, "reality") {
		b["tls"] = true
		b["servername"] = n.Handshake
		b["client-fingerprint"] = "chrome"
		b["reality-opts"] = M{"public-key": a.Public, "short-id": a.ShortID}
		if p.ID == "reality" {
			b["flow"] = "xtls-rprx-vision"
		} else {
			b["network"] = "grpc"
			b["grpc-opts"] = M{"grpc-service-name": "singforge"}
		}
	}
	return b
}
func uri(n model.Node) string {
	a := n.Auth
	host := net.JoinHostPort(n.Host, strconv.Itoa(n.Port))
	if n.Spec.Type == "shadowsocks" {
		userinfo := base64.RawURLEncoding.EncodeToString([]byte(method(n.Spec.ID) + ":" + a.Password))
		if n.Spec.ID == "ss2022" {
			userinfo = url.UserPassword(method(n.Spec.ID), a.Password).String()
		}
		return "ss://" + userinfo + "@" + host + "#" + n.ID
	}
	if n.Spec.TLS {
		q := url.Values{"sni": {n.SNI}}
		user := url.User(a.Password)
		if n.Spec.ID == "tuic" {
			user = url.UserPassword(a.UUID, a.Password)
			q.Set("alpn", "h3")
			q.Set("congestion_control", "cubic")
		}
		if n.Spec.ID == "trojan" {
			q.Set("security", "tls")
		}
		return (&url.URL{Scheme: n.Spec.Type, User: user, Host: host, RawQuery: q.Encode(), Fragment: n.ID}).String()
	}
	if n.Spec.Type == "vmess" {
		b, _ := JSON(M{"v": "2", "ps": n.ID, "add": n.Host, "port": strconv.Itoa(n.Port), "id": a.UUID, "aid": "0", "net": "ws", "host": n.Host, "path": a.Path, "tls": "tls", "sni": n.Host})
		return "vmess://" + base64.StdEncoding.EncodeToString(b)
	}
	q := url.Values{}
	user := a.UUID
	if n.Spec.Type == "trojan" {
		user = a.Password
	}
	if n.Spec.Type == "vless" {
		q.Set("encryption", "none")
	}
	if n.Spec.WS {
		q.Set("security", "tls")
		q.Set("type", "ws")
		q.Set("path", a.Path)
		q.Set("host", n.Host)
		q.Set("sni", n.Host)
	} else {
		q.Set("security", "reality")
		q.Set("sni", n.Handshake)
		q.Set("pbk", a.Public)
		q.Set("sid", a.ShortID)
		q.Set("fp", "chrome")
		if n.Spec.ID == "reality" {
			q.Set("type", "tcp")
			q.Set("flow", "xtls-rprx-vision")
		} else {
			q.Set("type", "grpc")
			q.Set("serviceName", "singforge")
		}
	}
	return (&url.URL{Scheme: n.Spec.Type, User: url.User(user), Host: host, RawQuery: q.Encode(), Fragment: n.ID}).String()
}
func Export(s model.State, format, filter string) ([]byte, []string, error) {
	var nodes []model.Node
	var warnings []string
	for _, n := range s.Nodes() {
		if filter != "" && filter != n.Mode && filter != n.ID {
			continue
		}
		ok := true
		switch format {
		case "sing-box":
			ok = n.Spec.ID != "naive" || n.PublicTLS
		case "mihomo", "provider":
			ok = n.Spec.ID != "naive" && n.Spec.ID != "snell" && n.Spec.ID != "shadowtls"
		case "uri", "v2ray":
			ok = n.Spec.WS || strings.HasPrefix(n.Spec.ID, "reality") || n.Spec.Type == "shadowsocks" || (n.PublicTLS && n.Spec.TLS && n.Spec.ID != "naive")
		case "shadowsocks":
			ok = n.Spec.Type == "shadowsocks"
		default:
			return nil, nil, fmt.Errorf("unknown format %q", format)
		}
		if ok {
			nodes = append(nodes, n)
		} else {
			warnings = append(warnings, n.ID+" omitted for "+format+" (transport or trust compatibility)")
		}
	}
	if len(nodes) == 0 {
		return nil, warnings, fmt.Errorf("no compatible enabled endpoints")
	}
	if format == "sing-box" {
		b, e := singClient(s, nodes)
		return b, warnings, e
	}
	if format == "mihomo" || format == "provider" {
		proxies := []M{}
		var bases []M
		tags := orderedMihomoTags(nodes)
		for _, n := range nodes {
			p := mihomo(n)
			proxies = append(proxies, p)
			if (n.Mode == "cdn" || n.Mode == "tunnel") && n.Spec.WS {
				bases = append(bases, p)
			}
		}
		cfg := M{"proxies": proxies}
		if format == "mihomo" {
			groups := []M{{"name": "Main", "type": "select", "proxies": append([]string{"Auto"}, tags...)}, {"name": "Auto", "type": "fallback", "lazy": false, "proxies": tags, "url": "https://www.gstatic.com/generate_204", "interval": 300}}
			ipv6, family := mihomoFamilies(s, nodes)
			if len(bases) > 0 {
				var eligible []string
				for _, h := range s.PreferredEdge.Addresses {
					if !ipv6 && strings.Contains(h, ":") {
						warnings = append(warnings, h+" omitted from Preferred Edge because client IPv6 is disabled")
						continue
					}
					eligible = append(eligible, h)
				}
				variants, names := edge.Variants(bases, eligible)
				cfg["proxies"] = append(proxies, variants...)
				groups[0]["proxies"] = append([]string{"Auto", edge.Group}, tags...)
				groups = append(groups, M{"name": edge.Group, "type": "fallback", "proxies": names, "url": "https://www.gstatic.com/generate_204", "expected-status": "204", "interval": 120, "timeout": 5000, "lazy": false})
			}
			for _, g := range []string{"AI", "AI-CN", "Streaming", "Communication"} {
				groups = append(groups, M{"name": g, "type": "select", "proxies": append([]string{"Main"}, tags...)})
			}
			pol, end := policyBeforeGeo(s)
			cfg["rules"] = mihomoRules(s, pol, end)
			cfg["proxy-groups"] = groups
			cfg["mixed-port"] = 7890
			cfg["allow-lan"] = false

			s.DNSFamily = family
			cfg["ipv6"] = ipv6
			cfg["mode"] = "rule"
			cfg["geodata-mode"] = true
			cfg["geo-auto-update"] = true
			cfg["geo-update-interval"] = 24
			cfg["geox-url"] = M{"geoip": "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip.dat", "geosite": "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat"}
			if s.Capture != "system" {
				cfg["tun"] = M{"enable": true, "stack": "mixed", "auto-route": true, "auto-detect-interface": true, "strict-route": true, "dns-hijack": []string{"any:53", "tcp://any:53"}}
			}
			dns := mihomoDNS(s, pol, ipv6)
			if s.MihomoNodeDNS == "alidns" {
				hosts := []string{}
				for _, n := range nodes {
					hosts = append(hosts, n.Host)
				}
				if len(bases) > 0 {
					hosts = append(hosts, s.PreferredEdge.Addresses...)
				}
				addNodeDNS(dns, hosts, family, s.DNSTransport)
			}
			cfg["dns"] = dns
		}
		b, e := yaml.Marshal(cfg)
		return b, warnings, e
	}
	lines := []string{}
	for _, n := range nodes {
		lines = append(lines, uri(n))
	}
	b := strings.Join(lines, "\n") + "\n"
	if format == "v2ray" {
		b = base64.StdEncoding.EncodeToString([]byte(b))
	}
	return []byte(b), warnings, nil
}
