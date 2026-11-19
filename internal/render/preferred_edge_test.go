package render

import (
	"reflect"
	"slices"
	"testing"

	"github.com/howardshaw/singforge/internal/edge"
	"github.com/howardshaw/singforge/internal/model"
	"gopkg.in/yaml.v3"
)

func TestPreferredEdgeExport(t *testing.T) {
	s, _ := model.New("direct.example.org")
	s.MihomoNodeDNS = "alidns"
	s.PreferredEdge = model.EdgePreferences{Mode: "domains", Addresses: []string{"first.example.org", "second.example.org"}}
	for _, c := range []struct {
		id, mode, host string
		port           int
	}{
		{"vless-ws", "tunnel", "tunnel.example.org", 443}, {"trojan-ws", "cdn", "cdn.example.org", 8443}, {"vmess-ws", "cdn", "cdn.example.org", 8443}, {"ss", "direct", s.Direct, 8388},
	} {
		if _, err := s.Enable(c.id); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(c.id, c.mode, c.host, c.port); err != nil {
			t.Fatal(err)
		}
	}
	type profile struct {
		Proxies []M
		Groups  []struct {
			Name, Type string
			Proxies    []string
		} `yaml:"proxy-groups"`
		DNS struct {
			Node   map[string][]string `yaml:"proxy-server-nameserver-policy"`
			Policy M                   `yaml:"nameserver-policy"`
		}
	}
	export := func(format, filter string) profile {
		t.Helper()
		b, _, err := Export(*s, format, filter)
		if err != nil {
			t.Fatal(err)
		}
		var p profile
		if err = yaml.Unmarshal(b, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := export("mihomo", "")
	if len(p.Proxies) != 10 {
		t.Fatal("expected six alternative addresses")
	}
	if p.Groups[0].Proxies[1] != edge.Group {
		t.Fatal("selector missing in Main")
	}
	g := p.Groups[2]
	want := []string{"tunnel-vless-ws", "cdn-trojan-ws", "cdn-vmess-ws"}
	if g.Name != edge.Group || g.Type != "url-test" || p.Groups[1].Type != "url-test" {
		t.Fatal("missing client latency selection")
	}
	names := proxyNames(p.Proxies)
	if !slices.Equal(p.Groups[0].Proxies[2:], names) || !slices.Equal(p.Groups[1].Proxies, names) || !slices.Equal(g.Proxies, names[:9]) {
		t.Fatal("candidate missing from manual or automatic selection")
	}
	for _, g := range p.Groups[3:] {
		if !slices.Equal(g.Proxies[1:], names) {
			t.Fatalf("incomplete %s selector", g.Name)
		}
	}
	for j, baseID := range want {
		base := p.Proxies[j*3]
		if base["name"] != baseID {
			t.Fatal("unexpected baseline order")
		}
		for i, addr := range s.PreferredEdge.Addresses {
			clone := p.Proxies[j*3+1+i]
			if clone["server"] != addr || clone["name"] != baseID+" / "+addr || len(p.DNS.Node[addr]) != 2 || p.DNS.Policy[addr] != nil {
				t.Fatal("wrong dial address or DNS scope")
			}
			clone["server"], clone["name"] = base["server"], base["name"]
			if !reflect.DeepEqual(clone, base) {
				t.Fatal("WS identity or credentials changed")
			}
		}
	}
	provider := export("provider", "")
	if len(provider.Groups) != 0 || !slices.Equal(proxyNames(provider.Proxies), names) {
		t.Fatal("incomplete provider")
	}
	direct := export("mihomo", "direct")
	if len(direct.Proxies) != 1 || slices.Contains(direct.Groups[0].Proxies, edge.Group) || len(direct.DNS.Node) != 1 {
		t.Fatal("selector leaked into direct-only export")
	}
	s.PreferredEdge = model.EdgePreferences{Mode: "ips", Addresses: []string{"188.114.96.1", "2606:4700::1"}}
	v4 := export("mihomo", "tunnel")
	if len(v4.Proxies) != 2 || v4.Proxies[1]["server"] != "188.114.96.1" || len(v4.DNS.Node) != 1 {
		t.Fatal("unexpected IPv4 or DNS selection")
	}
	s.MihomoIPv6 = "on"
	dual := export("mihomo", "tunnel")
	if len(dual.Proxies) != 3 || dual.Proxies[2]["server"] != "2606:4700::1" || dual.Proxies[2]["servername"] != "tunnel.example.org" {
		t.Fatal("invalid IPv6 edge")
	}
	s.PreferredEdge = model.EdgePreferences{}
	if g := export("mihomo", "").Groups[2]; !slices.Equal(g.Proxies, want) {
		t.Fatal("default mode contains third-party domains")
	}
}
