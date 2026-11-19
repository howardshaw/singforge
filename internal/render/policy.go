package render

import (
	"strings"

	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/routing"
)

func matches(kind, group string, values ...string) []routing.Rule {
	out := make([]routing.Rule, 0, len(values))
	for _, v := range values {
		out = append(out, routing.Rule{Kind: kind, Value: v, Group: group})
	}
	return out
}

var smartServices = func() []routing.Rule {
	rules := matches("IP-CIDR", "DIRECT", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16", "100.64.0.0/10")
	rules = append(rules, matches("IP-CIDR6", "DIRECT", "::1/128", "fc00::/7", "fe80::/10")...)
	rules = append(rules, matches("DOMAIN", "DIRECT", "captive.apple.com")...)
	rules = append(rules, matches("DOMAIN-SUFFIX", "DIRECT", "msftconnecttest.com", "msftncsi.com")...)
	rules = append(rules, matches("DOMAIN-SUFFIX", "REJECT", "doubleclick.net")...)
	rules = append(rules, matches("DOMAIN-SUFFIX", "AI",
		"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com", "anthropic.com", "claude.ai", "claudeusercontent.com",
		"gemini.google.com", "aistudio.google.com", "grok.com", "x.ai", "copilot.microsoft.com", "perplexity.ai",
		"deepseek.com", "moonshot.cn", "kimi.com", "qwen.ai", "tongyi.aliyun.com", "chatglm.cn", "bigmodel.cn", "doubao.com")...)
	rules = append(rules, matches("DOMAIN", "AI", "generativelanguage.googleapis.com")...)
	rules = append(rules, matches("DOMAIN-SUFFIX", "Streaming", "youtube.com", "youtu.be", "googlevideo.com", "ytimg.com", "netflix.com", "nflxvideo.net", "nflximg.net", "disneyplus.com", "primevideo.com", "max.com", "tv.apple.com", "spotify.com", "tiktok.com")...)
	return append(rules, matches("DOMAIN-SUFFIX", "Communication", "telegram.org", "telegram.me", "t.me", "whatsapp.com", "whatsapp.net", "discord.com", "discord.gg")...)
}()
var smartGeneral = func() []routing.Rule {
	rules := matches("DOMAIN-SUFFIX", "Main", "github.com", "githubusercontent.com", "githubassets.com", "google.com", "microsoft.com")
	rules = append(rules, matches("DOMAIN-SUFFIX", "DIRECT", "push.apple.com", "time.apple.com", "swscan.apple.com")...)
	rules = append(rules, matches("DOMAIN", "DIRECT", domesticHosts...)...)
	return append(rules, matches("DOMAIN-SUFFIX", "DIRECT", "cn")...)
}()

// Keep the original Suffix field for consumers of rules list.
func BuiltinRules() []M {
	var out []M
	for _, set := range [][]routing.Rule{smartServices, smartGeneral} {
		for _, r := range set {
			v := M{"Kind": r.Kind, "Value": r.Value, "Group": r.Group}
			if r.Kind == "DOMAIN-SUFFIX" {
				v["Suffix"] = r.Value
			}
			out = append(out, v)
		}
	}
	return out
}
func policyBeforeGeo(s model.State) ([]routing.Rule, string) {
	out := append([]routing.Rule(nil), s.Overrides...)
	switch s.Routing {
	case "smart":
		out = append(out, smartServices...)
		for _, r := range s.Rules.Rules {
			if r.Group != "DIRECT" && r.Group != "Main" {
				out = append(out, r)
			}
		}
		out = append(out, smartGeneral...)
	case "gfw-only":
		for _, r := range s.Rules.Rules {
			if r.Group == "Main" {
				out = append(out, r)
			}
		}
		return out, "DIRECT"
	}
	return out, "Main"
}
func policy(s model.State) ([]routing.Rule, string) {
	out, end := policyBeforeGeo(s)
	if s.Routing == "smart" {
		// Same-action terminal predicates can be grouped by kind in native JSON.
		for _, kind := range []string{"DOMAIN", "DOMAIN-SUFFIX", "IP-CIDR", "IP-CIDR6"} {
			for _, r := range s.Rules.Rules {
				if r.Group == "DIRECT" && r.Kind == kind {
					out = append(out, r)
				}
			}
		}
	}
	return out, end
}
func mihomoRules(cfg M, s model.State, policy []routing.Rule, end string) []string {
	rules := make([]string, 0, len(policy)+3)
	for _, r := range policy {
		line := r.Kind + "," + r.Value + "," + r.Group
		if strings.HasPrefix(r.Kind, "IP-CIDR") {
			line += ",no-resolve"
		}
		rules = append(rules, line)
	}
	if s.Routing == "smart" {
		var domains, cidrs []string
		for _, r := range s.Rules.Rules {
			if r.Group != "DIRECT" {
				continue
			}
			switch r.Kind {
			case "DOMAIN":
				domains = append(domains, r.Value)
			case "DOMAIN-SUFFIX":
				domains = append(domains, "+."+r.Value)
			case "IP-CIDR", "IP-CIDR6":
				cidrs = append(cidrs, r.Value)
			}
		}
		providers := M{}
		if len(domains) > 0 {
			providers["cn-domains"] = M{"type": "inline", "behavior": "domain", "payload": domains}
			rules = append(rules, "RULE-SET,cn-domains,DIRECT")
		}
		if len(cidrs) > 0 {
			providers["cn-ips"] = M{"type": "inline", "behavior": "ipcidr", "payload": cidrs}
			rules = append(rules, "RULE-SET,cn-ips,DIRECT,no-resolve")
		}
		if len(providers) > 0 {
			cfg["rule-providers"] = providers
		}
	}
	return append(rules, "MATCH,"+end)
}
