package render

import (
	"encoding/json"
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
	for k, w := range map[string]string{"gemini.google.com": "REJECT", "mail.google.com": "Main", "foo.openai.com": "AI", "notopenai.com": "Main"} {
		if !strings.HasPrefix(Explain(*s, k), w) {
			t.Fatal(k)
		}
	}
}
