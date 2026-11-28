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
		"openai.com", "chatgpt.com", "chat.com", "oaistatic.com", "oaistatsig.com", "oaiusercontent.com", "sora.com", "chatgpt.livekit.cloud",
		"anthropic.com", "claude.ai", "claudeusercontent.com", "claude.com", "claude.app", "claude.site", "claudemcpcontent.com", "claudemcpclient.com",
		"gemini.google", "gemini.gstatic.com", "ai.studio", "makersuite.google.com", "generativeai.google", "gemini.google.com", "aistudio.google.com", "ai.google.dev", "bard.google.com",
		"notebooklm.google.com", "notebooklm.google", "antigravity.google", "antigravity-unleash.goog",
		"cursor.com", "cursor.sh", "cursorapi.com", "cursor-cdn.com", "cursorvm.com",
		"grok.com", "grokusercontent.com", "x.ai", "copilot.microsoft.com", "githubcopilot.com", "perplexity.ai", "perplexity.com", "pplx.ai")...)
	rules = append(rules, matches("DOMAIN", "AI",
		"geminiweb-pa.clients.google.com", "geminiweb-pa.clients6.google.com", "robinfrontend-pa.googleapis.com",
		"appsgenaiserver-pa.clients.google.com", "appsgenaiserver-pa.clients6.google.com", "appsgenaiserver-pa.googleapis.com",
		"alkalicore-pa.clients6.google.com", "alkalimakersuite-pa.clients6.google.com", "webchannel-alkalimakersuite-pa.clients6.google.com",
		"antigravity-pa.googleapis.com", "antigravity.googleapis.com", "notebooklm.googleapis.com",
		"generativelanguage.googleapis.com", "jnn-pa.googleapis.com", "waa-pa.clients6.google.com",
		"cloudaicompanion.googleapis.com", "cloudcode-pa.googleapis.com", "daily-cloudcode-pa.googleapis.com",
		"daily-cloudcode-pa.sandbox.googleapis.com", "aicode.googleapis.com", "notebooklm-pa.googleapis.com",
		"businessaicode.googleapis.com", "businessaicode.us.rep.googleapis.com", "businessaicode.eu.rep.googleapis.com", "businessaicode.mtls.googleapis.com",
		"aiplatform.googleapis.com", "aiplatform.us.rep.googleapis.com", "aiplatform.eu.rep.googleapis.com", "aiplatform.mtls.googleapis.com",
		"discoveryengine.googleapis.com", "us-discoveryengine.googleapis.com", "eu-discoveryengine.googleapis.com",
		"discoveryengine.mtls.googleapis.com", "us-discoveryengine.mtls.googleapis.com", "eu-discoveryengine.mtls.googleapis.com",
		"ppl-ai-file-upload.s3.amazonaws.com", "pplx-res.cloudinary.com", "openaiassets.blob.core.windows.net",
		"accounts.spacexsi.com", "auth.grokipedia.com", "copilot-proxy.githubusercontent.com", "copilot-telemetry.githubusercontent.com", "origin-tracker.githubusercontent.com")...)
	rules = append(rules, matches("DOMAIN-SUFFIX", "AI-CN", "deepseek.com", "moonshot.cn", "kimi.com", "qwen.ai", "tongyi.aliyun.com", "chatglm.cn", "bigmodel.cn", "doubao.com")...)
	rules = append(rules, matches("DOMAIN-SUFFIX", "Streaming", "youtube.com", "youtu.be", "googlevideo.com", "ytimg.com", "netflix.com", "nflxvideo.net", "nflximg.net", "disneyplus.com", "primevideo.com", "max.com", "tv.apple.com", "spotify.com", "tiktok.com")...)
	return append(rules, matches("DOMAIN-SUFFIX", "Communication", "telegram.org", "telegram.me", "t.me", "whatsapp.com", "whatsapp.net", "discord.com", "discord.gg")...)
}()
var smartGeneral = func() []routing.Rule {
	rules := matches("DOMAIN-SUFFIX", "Main", "github.com", "githubusercontent.com", "githubassets.com", "google.com", "microsoft.com")
	// Shared Gemini assets and sign-in APIs can otherwise reach geographic DIRECT rules.
	rules = append(rules, matches("DOMAIN", "Main", "oauth2.googleapis.com", "www.googleapis.com", "serviceusage.googleapis.com",
		"cloudresourcemanager.googleapis.com", "people.googleapis.com", "firebaselogging-pa.googleapis.com",
		"lh3.googleusercontent.com", "lh5.googleusercontent.com", "ssl.gstatic.com", "fonts.googleapis.com",
		"maps.gstatic.com", "fonts.gstatic.com", "maps.googleapis.com", "www.gstatic.com", "yt3.ggpht.com",
		"csp.withgoogle.com", "www.googletagmanager.com", "www.google-analytics.com", "optimizationguide-pa.googleapis.com",
		"encrypted-tbn0.gstatic.com", "encrypted-tbn1.gstatic.com", "encrypted-tbn2.gstatic.com", "encrypted-tbn3.gstatic.com",
		"streetviewpixels-pa.googleapis.com", "content-autofill.googleapis.com")...)
	rules = append(rules, matches("DOMAIN-SUFFIX", "DIRECT", "push.apple.com", "time.apple.com", "swscan.apple.com")...)
	return rules
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

const geoAI = "category-ai-!cn"
const geoAICN = "category-ai-cn"

// Only the full Mihomo profile consumes the client GeoSite database.
func profilePolicy(s model.State, mihomo bool) ([]routing.Rule, string) {
	out := append([]routing.Rule(nil), s.Overrides...)
	switch s.Routing {
	case "smart":
		out = append(out, smartServices...)
		for _, r := range s.Rules.Rules {
			if r.Group != "DIRECT" && r.Group != "Main" {
				out = append(out, r)
			}
		}
		if mihomo {
			out = append(out, routing.Rule{Kind: "GEOSITE", Value: geoAI, Group: "AI"},
				routing.Rule{Kind: "GEOSITE", Value: geoAICN, Group: "AI-CN"})
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
func policy(s model.State) ([]routing.Rule, string) { return profilePolicy(s, false) }

func mihomoRules(s model.State, policy []routing.Rule) []string {
	rules := make([]string, 0, len(policy)+4)
	for _, r := range policy {
		line := r.Kind + "," + r.Value + "," + r.Group
		if strings.HasPrefix(r.Kind, "IP-CIDR") {
			line += ",no-resolve"
		}
		rules = append(rules, line)
	}
	if s.Routing == "smart" {
		rules = append(rules, "GEOSITE,CN,DIRECT")
		if s.MihomoPolicy == "domestic-first" {
			rules = append(rules, "GEOSITE,geolocation-!cn,Main")
		}
		rules = append(rules, "GEOIP,CN,DIRECT,no-resolve")
	}
	return append(rules, "MATCH,Unmatched")
}
