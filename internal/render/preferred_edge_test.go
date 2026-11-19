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
	if g.Name != edge.Group || g.Type != "fallback" || !slices.Equal(g.Proxies[6:], want) {
		t.Fatal("baseline order", g)
	}
	for i, addr := range s.PreferredEdge.Addresses {
		if len(p.DNS.Node[addr]) != 2 || p.DNS.Policy[addr] != nil {
			t.Fatal("incorrect DNS scope")
		}
		for j := range want {
			clone := p.Proxies[4+i*len(want)+j]
			if clone["server"] != addr || clone["name"] != g.Proxies[i*len(want)+j] {
				t.Fatal("candidate priority")
			}
			clone["server"] = p.Proxies[j]["server"]
			clone["name"] = p.Proxies[j]["name"]
			if !reflect.DeepEqual(clone, p.Proxies[j]) {
				t.Fatal("WS identity or credentials changed")
			}
		}
	}
	if len(export("provider", "").Proxies) != 4 {
		t.Fatal("full-profile selector leaked into provider")
	}
	direct := export("mihomo", "direct")
	if len(direct.Proxies) != 1 || slices.Contains(direct.Groups[0].Proxies, edge.Group) || len(direct.DNS.Node) != 1 {
		t.Fatal("selector leaked into direct-only export")
	}
	s.PreferredEdge = model.EdgePreferences{Mode: "ips", Addresses: []string{"104.16.132.229", "2606:4700::1"}}
	v4 := export("mihomo", "tunnel")
	if len(v4.Proxies) != 2 || v4.Proxies[1]["server"] != "104.16.132.229" || len(v4.DNS.Node) != 1 {
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
