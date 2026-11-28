package render

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/routing"
)

func TestPrivacyAndExports(t *testing.T) {
	s, _ := model.New("2001:db8::2")
	for _, p := range model.Catalog {
		a, e := s.Enable(p.ID)
		if e != nil {
			t.Fatal(e)
		}
		mode, h := "direct", s.Direct
		if p.WS {
			mode, h = "cdn", "cdn.example.org"
		}
		if e = s.Add(p.ID, mode, h, a.Port); e != nil {
			t.Fatal(e)
		}
	}
	for _, format := range []string{"mihomo", "provider", "sing-box", "uri", "v2ray", "shadowsocks"} {
		b, _, e := Export(*s, format, "")
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(b), "insecure: true") || strings.Contains(string(b), s.Key) {
			t.Fatal("unsafe export")
		}
		var cfg M
		switch format {
		case "mihomo":
			if e = yaml.Unmarshal(b, &cfg); e != nil {
				t.Fatal(e)
			}
			dns := cfg["dns"].(map[string]any)
			if !strings.Contains(dns["nameserver"].([]any)[0].(string), "#Main") || cfg["allow-lan"] != false {
				t.Fatal("privacy defaults")
			}
		case "sing-box":
			if e = json.Unmarshal(b, &cfg); e != nil {
				t.Fatal(e)
			}
			if cfg["dns"].(map[string]any)["final"] != "remote" {
				t.Fatal("DNS routing")
			}
		case "shadowsocks":
			for _, l := range strings.Fields(string(b)) {
				if !strings.HasPrefix(l, "ss://") || !strings.Contains(l, "@[2001:db8::2]:") {
					t.Fatal("incompatible SS URI")
				}
			}
		}
	}
	s.Overrides = []routing.Rule{{Kind: "DOMAIN", Value: "gemini.google.com", Group: "REJECT"}}
	for k, w := range map[string]string{"gemini.google.com": "REJECT", "mail.google.com": "Main", "foo.openai.com": "AI", "notopenai.com": "Client"} {
		if !strings.HasPrefix(Explain(*s, k), w) {
			t.Fatal(k)
		}
	}
}

func TestMihomoUnmatchedSelector(t *testing.T) {
	s, _ := model.New("192.0.2.1")
	inbound, err := s.Enable("ss")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Add("ss", "direct", s.Direct, inbound.Port); err != nil {
		t.Fatal(err)
	}
	for _, preset := range []string{"smart", "global", "custom", "gfw-only"} {
		s.Routing = preset
		data, _, err := Export(*s, "mihomo", "")
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			Rules  []string
			Groups []struct {
				Name, Type string
				Proxies    []string
			} `yaml:"proxy-groups"`
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			t.Fatal(err)
		}
		want := []string{"Main", "DIRECT"}
		if preset == "gfw-only" {
			want = []string{"DIRECT", "Main"}
		}
		if cfg.Rules[len(cfg.Rules)-1] != "MATCH,Unmatched" {
			t.Fatalf("%s final rule: %v", preset, cfg.Rules)
		}
		found := false
		for _, group := range cfg.Groups {
			if group.Name == "Unmatched" {
				found = group.Type == "select" && slices.Equal(group.Proxies, want)
			}
		}
		if !found {
			t.Fatalf("%s missing selector with choices %v", preset, want)
		}
	}
}
