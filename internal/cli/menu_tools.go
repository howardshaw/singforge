package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/skip2/go-qrcode"
)

var toolMenus = map[string][]choice{
	"remove-options": {
		{"Preview full local uninstall", "预览完整本机卸载", "uninstall --full --dry-run"},
		{"Detach services and keep state", "卸载服务并保留状态", "uninstall --yes"},
		{"Purge active state and keep CLI", "清除当前状态并保留 CLI", "uninstall --purge --yes"},
		{"Full local uninstall including CLI", "完整本机卸载（含 CLI）", "uninstall --full --yes"},
	},
	"subscriptions": {
		{"Publication status", "发布状态", "subscription list"},
		{"Generate subscription URL / QR", "生成订阅地址 / 二维码", "formats"},
		{"Generate node QR (offline)", "生成节点二维码（离线）", "subscription nodes"},
		{"Enable HTTPS publication", "启用 HTTPS 订阅发布", "publish"},
		{"Automatic certificate renewal", "配置证书自动续签", "renewal-enable"},
		{"Renewal status", "证书续签状态", "certificate auto-renew status"},
		{"Test renewal with staging CA", "测试证书续签", "certificate auto-renew test"},
		{"Preferred Edge selector", "Cloudflare 优选入口", "preferred-edges"},
	},
	"preferred-edges": {
		{"Current edge settings", "查看当前优选设置", "edge status"},
		{"Publish all domain and IP nodes", "发布全部优选域名和 IP 节点", "edge set --mode all"},
		{"Publish preferred IP nodes", "发布优选 IP 节点", "edge set --mode ips"},
		{"Publish community domain nodes", "发布社区域名节点", "edge set --mode domains"},
		{"Use default Cloudflare endpoints", "使用默认 Cloudflare 入口", "edge set --mode default"},
	},
	"formats": {
		{"Clash Verge Rev / Mihomo", "Clash Verge Rev / Mihomo", "issue mihomo"},
		{"Shadowsocks / SS", "Shadowsocks / SS", "issue shadowsocks"},
		{"v2rayN / V2Ray", "v2rayN / V2Ray", "issue v2ray"},
		{"sing-box", "sing-box", "issue sing-box"},
	},
	"updates": {
		{"Update sing-box core", "更新 sing-box 内核", "core update"},
		{"Update cloudflared connector", "更新 cloudflared 连接器", "cloudflared update"},
		{"Update SingForge CLI", "更新 SingForge CLI", "self update"},
		{"Update all components", "更新全部组件", "update --all"},
	},
}

type menuLocation struct {
	page, title string
	cursor      int
}
type menuTools struct {
	trail                   []menuLocation
	pending                 []string
	resultKind, payload, qr string
	nodeChoices             []choice
	fields                  []string
	fieldIndex              int
}

func (m *dashboard) push(page, title string) {
	m.trail = append(m.trail, menuLocation{m.page, m.title, m.cursor})
	m.page = page
	m.title = title
	m.cursor = 0
	m.scroll = 0
	m.problem = ""
}
func (m *dashboard) pop() {
	last := len(m.trail) - 1
	l := m.trail[last]
	m.trail = m.trail[:last]
	m.page = l.page
	m.cursor = l.cursor
	m.title = l.title
	m.scroll = 0
	m.problem = ""
	m.body = ""
	m.pending = nil
	m.resultKind = ""
	m.payload = ""
	m.qr = ""
	m.entry = ""
}
func (m *dashboard) acceptTool(item choice) (tea.Cmd, bool) {
	a := item.action
	title := m.t(item.en, item.zh)
	if _, ok := toolMenus[a]; ok {
		m.push(a, title)
		return nil, true
	}
	if cmd, ok := m.acceptCommand(item); ok {
		return cmd, true
	}
	switch m.page {
	case "remove-options":
		args := strings.Fields(a)
		if a == "uninstall --full --dry-run" {
			m.push("busy", title)
			return m.command(args...), true
		}
		m.push("tool-confirm", title)
		m.pending = args
		return nil, true
	case "subscriptions":
		if a == "renewal-enable" {
			m.push("renewal-credentials", title)
			m.fieldIndex = 0
			m.entry = "/etc/letsencrypt/singforge-dns.ini"
			return nil, true
		}
		if a == "certificate auto-renew test" {
			m.push("tool-confirm", title)
			m.pending = strings.Fields(a)
			return nil, true
		}
		if a == "publish" {
			m.push("publication-form", title)
			m.fields = make([]string, 3)
			m.fieldIndex = 0
			m.entry = ""
			return nil, true
		}
		m.push("busy", title)
		m.resultKind = a
		return m.command(strings.Fields(a)...), true
	case "preferred-edges":
		if a == "edge status" {
			m.push("busy", title)
			return m.command("edge", "status"), true
		}
		m.push("tool-confirm", title)
		m.pending = strings.Fields(a)
		return nil, true
	case "formats":
		m.push("tool-confirm", title)
		m.pending = []string{"subscription", "issue", "--format", strings.TrimPrefix(a, "issue ")}
		m.resultKind = "issued"
		return nil, true
	case "nodes":
		m.push("busy", title)
		m.resultKind = "node"
		return m.command("subscription", "show", "--format", "uri", "--filter", a), true
	case "updates":
		m.push("tool-confirm", title)
		m.pending = append(strings.Fields(a), "--yes")
		return nil, true
	}
	return nil, false
}
func (m *dashboard) toolResult(v menuResult) bool {
	if v.err != nil {
		return false
	}
	switch m.resultKind {
	case "subscription nodes":
		var ids []string
		if json.Unmarshal([]byte(v.body), &ids) != nil {
			return false
		}
		m.nodeChoices = nil
		for _, id := range ids {
			m.nodeChoices = append(m.nodeChoices, choice{id, id, id})
		}
		if len(ids) == 0 {
			m.page = "detail"
			m.body = m.t("No URI-compatible enabled endpoints.", "当前没有可导出的节点。")
		} else {
			m.page = "nodes"
			m.cursor = 0
		}
		return true
	case "issued", "node":
		value := v.body
		if m.resultKind == "issued" {
			var envelope struct {
				URL string `json:"issued_url"`
			}
			if json.Unmarshal([]byte(value), &envelope) == nil {
				value = envelope.URL
			}
		}
		u, e := url.Parse(value)
		if e != nil || u.Scheme == "" || u.Host == "" {
			return false
		}
		m.payload = value
		m.body = value
		m.page = "detail"
		q, e := qrcode.New(value, qrcode.Medium)
		if e == nil {
			m.qr = strings.TrimSuffix(q.ToSmallString(false), "\n")
			if m.resultKind == "node" {
				m.page = "qr"
			}
		}
		return true
	}
	return false
}
func (m *dashboard) saveQR(path string) error {
	q, e := qrcode.New(m.payload, qrcode.Medium)
	if e != nil {
		return e
	}
	data, e := q.PNG(512)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	return f.Close()
}
func (m *dashboard) inputTool(key, text string) (tea.Cmd, bool) {
	switch m.page {
	case "flag-value":
		switch key {
		case "esc":
			m.pop()
		case "enter":
			m.values[m.flagName] = m.entry
			if m.entry == "" {
				delete(m.values, m.flagName)
			}
			m.pop()
		case "backspace":
			r := []rune(m.entry)
			if len(r) > 0 {
				m.entry = string(r[:len(r)-1])
			}
		default:
			m.entry = appendForm(m.entry, text)
		}
		return nil, true
	case "publication-form", "png-path", "renewal-credentials":
		switch key {
		case "esc":
			if m.page == "png-path" {
				m.page = "qr"
				m.entry = ""
				m.problem = ""
			} else if m.fieldIndex > 0 {
				m.fieldIndex--
				m.entry = m.fields[m.fieldIndex]
			} else {
				m.pop()
			}
		case "backspace":
			r := []rune(m.entry)
			if len(r) > 0 {
				m.entry = string(r[:len(r)-1])
			}
		case "enter":
			p := strings.TrimSpace(m.entry)
			if p == "" {
				m.problem = m.t("Enter a value.", "请输入值。")
				break
			}
			if m.page == "renewal-credentials" {
				m.pending = []string{"certificate", "auto-renew", "enable", "--credentials", p, "--yes"}
				m.page = "tool-confirm"
				return nil, true
			}
			if m.page == "png-path" {
				if e := m.saveQR(p); e != nil {
					m.problem = e.Error()
					break
				}
				m.page = "detail"
				m.body = m.t("Private PNG saved: ", "二维码已保存：") + p
				m.payload = ""
				m.qr = ""
				break
			}
			m.fields[m.fieldIndex] = p
			m.fieldIndex++
			m.problem = ""
			m.entry = ""
			if m.fieldIndex == len(m.fields) {
				m.pending = []string{"subscription", "publish", "--host", m.fields[0], "--cert", m.fields[1], "--key", m.fields[2]}
				m.page = "tool-confirm"
			} else {
				m.entry = m.fields[m.fieldIndex]
			}
		default:
			m.entry = appendForm(m.entry, text)
		}
		return nil, true
	case "tool-confirm":
		if key == "enter" {
			m.page = "busy"
			return m.command(m.pending...), true
		}
		if key != "esc" && key != "q" {
			return nil, true
		}
	case "qr":
		switch key {
		case "p":
			m.page = "png-path"
			m.entry = ""
			m.problem = ""
		case "enter", "r":
			m.page = "detail"
			m.body = m.payload
		}
		if key != "q" && key != "esc" {
			return nil, true
		}
	case "detail":
		if m.qr != "" && key == "r" {
			m.page = "qr"
			return nil, true
		}
	}
	return nil, false
}
func appendForm(s, text string) string {
	for _, r := range cleanText(text) {
		if r >= ' ' && len(s) < 4096 {
			s += string(r)
		}
	}
	return s
}
func (m dashboard) toolView() (tea.View, bool) {
	if m.page != "qr" {
		return tea.View{}, false
	}
	rows := strings.Split(m.qr, "\n")
	width := ansi.StringWidth(rows[0])
	var b strings.Builder
	fmt.Fprintln(&b, "  "+m.title)
	if width+2 > m.width || len(rows)+4 > m.height {
		fmt.Fprintf(&b, "  %s %d × %d\n", m.t("Minimum terminal size:", "请将终端扩大至"), width+2, len(rows)+4)
	} else {
		for _, r := range rows {
			b.WriteString("  " + m.paint("97;40", r) + "\n")
		}
	}
	b.WriteString("  " + m.t("p save PNG   Enter show link   Esc back", "p 保存 PNG   Enter 查看链接   Esc 返回"))
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v, true
}
