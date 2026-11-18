package render

import (
	"encoding/json"
	"net"
	"strings"

	"github.com/howardshaw/singforge/internal/model"
)

type M = map[string]any

func JSON(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }
func method(id string) string {
	if id == "ss2022" {
		return "2022-blake3-aes-256-gcm"
	}
	return "aes-256-gcm"
}
func ws(path string) M { return M{"type": "ws", "path": path} }
func Server(s model.State) ([]byte, error) {
	ins := []M{}
	for _, a := range s.Inbounds {
		if !a.Enabled {
			continue
		}
		p, _ := model.Lookup(a.Protocol)
		b := M{"type": p.Type, "tag": p.ID, "listen": s.Listen, "listen_port": a.Port}
		u := M{"password": a.Password}
		switch p.Type {
		case "vless", "vmess":
			u = M{"uuid": a.UUID}
		case "tuic":
			u["uuid"] = a.UUID
		case "naive":
			u["username"] = "singforge"
			b["network"] = "tcp"
		case "snell":
			b["version"] = 6
			b["psk"] = a.Password
		case "shadowsocks":
			b["method"] = method(p.ID)
			b["password"] = a.Password
		case "shadowtls":
			b["version"] = 3
			b["handshake"] = M{"server": s.Handshake, "server_port": 443}
			b["detour"] = "shadow-inner"
			ins = append(ins, M{"type": "shadowsocks", "tag": "shadow-inner", "method": method("ss"), "password": a.UUID})
		}
		if p.Type != "shadowsocks" && p.Type != "snell" {
			b["users"] = []M{u}
		}
		if strings.HasPrefix(p.ID, "reality") {
			target, port, e := s.RealityDestination()
			if e != nil {
				return nil, e
			}
			b["tls"] = M{"enabled": true, "server_name": s.RealityName(), "reality": M{"enabled": true, "handshake": M{"server": target, "server_port": port}, "private_key": a.Private, "short_id": []string{a.ShortID}}}
			if p.ID == "reality" {
				u["flow"] = "xtls-rprx-vision"
			} else {
				b["transport"] = M{"type": "grpc", "service_name": "singforge"}
			}
		}
		if p.TLS {
			b["tls"] = M{"enabled": true, "certificate": s.Certificate, "key": s.Key}
			if p.ID == "tuic" {
				b["tls"].(M)["alpn"] = []string{"h3"}
			}
		}
		if p.WS {
			b["listen"] = "127.0.0.1"
			b["transport"] = ws(a.Path)
		}
		ins = append(ins, b)
	}
	addr := "1.1.1.1"
	if ip := net.ParseIP(s.Direct); ip != nil && ip.To4() == nil || s.ServerDNSFamily == "ipv6" {
		addr = "2606:4700:4700::1111"
	}
	if s.ServerDNSFamily == "ipv4" {
		addr = "1.1.1.1"
	}
	dns := M{"servers": []M{{"type": "https", "tag": "egress-dns", "server": addr, "tls": M{"enabled": true, "server_name": "cloudflare-dns.com"}}}, "final": "egress-dns"}
	return JSON(M{"dns": dns, "route": M{"default_domain_resolver": "egress-dns", "rules": []M{{"action": "resolve", "server": "egress-dns"}}}, "log": M{"level": "warn"}, "inbounds": ins, "outbounds": []M{{"type": "direct", "tag": "egress"}}})
}
func outbound(n model.Node) M {
	p, a := n.Spec, n.Auth
	b := M{"type": p.Type, "tag": n.ID, "server": n.Host, "server_port": n.Port}
	switch p.Type {
	case "vless", "vmess":
		b["uuid"] = a.UUID
	case "tuic":
		b["uuid"] = a.UUID
		b["password"] = a.Password
	case "shadowsocks":
		b["password"] = a.Password
		b["method"] = method(p.ID)
	case "snell":
		b["psk"] = a.Password
		b["version"] = 6
	case "naive":
		b["username"] = "singforge"
		b["password"] = a.Password
	default:
		b["password"] = a.Password
	}
	if p.TLS || p.WS {
		t := M{"enabled": true, "server_name": n.SNI}
		if p.TLS {
			t["certificate"] = n.Certificate
		}
		if p.ID == "tuic" {
			t["alpn"] = []string{"h3"}
		}
		b["tls"] = t
	}
	if p.WS {
		b["transport"] = ws(a.Path)
	}
	if strings.HasPrefix(p.ID, "reality") {
		b["tls"] = M{"enabled": true, "server_name": n.Handshake, "utls": M{"enabled": true, "fingerprint": "chrome"}, "reality": M{"enabled": true, "public_key": a.Public, "short_id": a.ShortID}}
		if p.ID == "reality" {
			b["flow"] = "xtls-rprx-vision"
		} else {
			b["transport"] = M{"type": "grpc", "service_name": "singforge"}
		}
	}
	return b
}
