package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/howardshaw/singforge/internal/model"
)

type choice struct{ en, zh, action string }

var homeChoices = []choice{
	{"Server overview", "服务器概览", "status"}, {"Public endpoints", "连接入口", "endpoint list"}, {"Protocol catalog", "协议目录", "protocol list"}, {"Subscriptions & QR codes", "订阅与二维码", "subscriptions"}, {"Health check", "运行诊断", "doctor"}, {"Component updates", "组件更新", "updates"}, {"Install or reapply", "安装与部署", "install"}, {"BBR and kernel tuning", "BBR 与内核调优", "cmd:bbr"}, {"Uninstall SingForge", "卸载 SingForge", "remove-options"}, {"All commands", "全部命令", "cmd:"}, {"Command reference", "命令参考", "--help"}, {"Exit", "退出", "quit"},
}
var presets = []choice{{"Recommended", "推荐组合", "recommended"}, {"Minimal", "精简组合", "minimal"}, {"Full", "完整组合", "full"}, {"Custom", "自选协议", "custom"}}

type menuResult struct {
	body string
	err  error
}
type dashboard struct {
	menuTools
	commandMenu
	panel                                           hostPanel
	digits                                          string
	ctx                                             context.Context
	args                                            []string
	binary                                          string
	zh, color                                       bool
	width, height, cursor, home, scroll             int
	page, title, body, preset, host, entry, problem string
}

func (m dashboard) t(en, zh string) string {
	if m.zh {
		return zh
	}
	return en
}
func (m dashboard) paint(code, text string) string {
	if !m.color {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}
func (m dashboard) choices() []choice {
	if strings.HasPrefix(m.page, "cmd:") {
		return m.commandChoices()
	}
	if m.page == "preset" {
		return presets
	}
	if c, ok := toolMenus[m.page]; ok {
		return c
	}
	if m.page == "nodes" {
		return m.nodeChoices
	}
	return homeChoices
}
func (m dashboard) Init() tea.Cmd { return m.refreshPanel() }
func cleanText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, ansi.Strip(s))
}
func (m dashboard) command(args ...string) tea.Cmd {
	return func() tea.Msg {
		all := append(slices.Clone(m.args), args...)
		c := exec.CommandContext(m.ctx, m.binary, all...)
		c.Env = append(os.Environ(), "NO_COLOR=1", "SINGFORGE_LANG="+m.t("en", "zh"))
		b, e := c.CombinedOutput()
		s := strings.TrimSpace(cleanText(string(b)))
		if e == nil {
			s = m.describe(strings.Join(args, " "), s)
		}
		return menuResult{s, e}
	}
}
func (m dashboard) describe(action, raw string) string {
	var rows []string
	switch action {
	case "status":
		var s struct {
			Endpoints        []model.Endpoint
			Services         map[string]string
			DNS, Publication string
		}
		if json.Unmarshal([]byte(raw), &s) != nil {
			return raw
		}
		keys := make([]string, 0, len(s.Services))
		for k := range s.Services {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			rows = append(rows, k+"  ·  "+s.Services[k])
		}
		pub := s.Publication
		if pub == "" {
			pub = m.t("Local exports only", "仅本地导出")
		}
		rows = append(rows, "", fmt.Sprintf("%s  %d", m.t("Configured endpoints", "已配置入口"), len(s.Endpoints)), "DNS  "+s.DNS, m.t("Subscriptions", "订阅")+"  "+pub)
	case "endpoint list":
		var eps []model.Endpoint
		if json.Unmarshal([]byte(raw), &eps) != nil {
			return raw
		}
		for _, ep := range eps {
			rows = append(rows, ep.ID, "  "+net.JoinHostPort(ep.Host, strconv.Itoa(ep.Port))+"  ·  "+ep.Mode, "")
		}
	case "protocol list":
		var catalog []model.Spec
		if json.Unmarshal([]byte(raw), &catalog) != nil {
			return raw
		}
		for _, p := range catalog {
			rows = append(rows, fmt.Sprintf("%-14s %s / %d", p.ID, strings.ToUpper(strings.Join(p.Networks(), "+")), p.Port))
		}
	case "subscription list":
		var b struct {
			Host   string
			Grants int
		}
		if json.Unmarshal([]byte(raw), &b) != nil {
			return raw
		}
		if b.Host == "" {
			return m.t("HTTPS publication is off. Choose Enable HTTPS publication for subscription links, or Generate node QR for offline import.", "公开订阅已关闭。生成订阅地址前请先启用 HTTPS 发布；离线导入可选生成节点二维码。")
		}
		rows = append(rows, "HTTPS  "+b.Host, fmt.Sprintf("%s  %d", m.t("Recorded grants", "授权记录"), b.Grants))
	default:
		return raw
	}
	if len(rows) == 0 {
		return m.t("No entries", "暂无记录")
	}
	return strings.TrimSpace(strings.Join(rows, "\n"))
}
func (m dashboard) back() dashboard {
	if len(m.trail) > 0 {
		m.pop()
		return m
	}
	m.problem = ""
	m.scroll = 0
	switch m.page {
	case "host", "custom", "confirm":
		m.page = "preset"
		m.cursor = 0
	default:
		m.page = "home"
		m.cursor = m.home
	}
	return m
}
func (m dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case hostPanel:
		m.panel = v
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
	case menuResult:
		if m.toolResult(v) {
			return m, nil
		}
		m.page = "detail"
		m.body = v.body
		if v.err != nil && m.body == "" {
			m.body = v.err.Error()
		}
		m.scroll = 0
	case tea.PasteMsg:
		if m.page == "flag-value" || m.page == "publication-form" || m.page == "png-path" || m.page == "renewal-credentials" {
			m.entry = appendForm(m.entry, v.Content)
		}
		if m.page == "host" || m.page == "custom" {
			m.entry = appendEntry(m.entry, v.Content)
		}
	case tea.KeyPressMsg:
		key := v.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.page == "busy" {
			return m, nil
		}
		if cmd, ok := m.inputTool(key, v.Text); ok {
			return m, cmd
		}
		if key == "esc" {
			if m.page == "home" {
				return m, tea.Quit
			}
			m = m.back()
			m.digits = ""
			if m.page == "home" {
				return m, m.refreshPanel()
			}
			return m, nil
		}
		if m.listPage() {
			if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
				m.digits += key
				n, _ := strconv.Atoi(m.digits)
				items := m.choices()
				if n > len(items) || len(m.digits) > 2 {
					m.digits = key
					n = int(key[0] - '0')
				}
				for i := range items {
					if m.number(i) == n {
						m.cursor = i
						break
					}
				}
				return m, nil
			}
			m.digits = ""
		}
		if m.page == "host" || m.page == "custom" {
			switch key {
			case "backspace":
				r := []rune(m.entry)
				if len(r) > 0 {
					m.entry = string(r[:len(r)-1])
				}
			case "enter":
				if m.page == "host" {
					m.host = strings.TrimSpace(m.entry)
					if !model.Host(m.host) {
						m.problem = m.t("Enter a valid IP or DNS hostname.", "请输入有效的 IP 或域名。")
						break
					}
					m.entry = ""
					m.problem = ""
					if m.preset == "custom" {
						m.page = "custom"
					} else {
						m.page = "confirm"
					}
				} else {
					for _, id := range strings.Split(m.entry, ",") {
						if !slices.Contains(ids(), id) {
							m.problem = m.t("Unknown protocol ID.", "协议 ID 无效。")
							return m, nil
						}
					}
					m.problem = ""
					m.page = "confirm"
				}
			default:
				m.entry = appendEntry(m.entry, v.Text)
			}
			return m, nil
		}
		if key == "q" {
			if m.page == "home" {
				return m, tea.Quit
			}
			m = m.back()
			m.digits = ""
			if m.page == "home" {
				return m, m.refreshPanel()
			}
			return m, nil
		}
		if key == "l" || key == "L" {
			m.zh = !m.zh
			return m, nil
		}
		if m.page == "detail" {
			limit := max(0, len(m.lines())-m.capacity())
			switch key {
			case "up", "k":
				m.scroll = max(0, m.scroll-1)
			case "down", "j":
				m.scroll = min(limit, m.scroll+1)
			case "pgdown", " ":
				m.scroll = min(limit, m.scroll+m.capacity())
			case "pgup":
				m.scroll = max(0, m.scroll-m.capacity())
			case "enter":
				m = m.back()
				m.digits = ""
				if m.page == "home" {
					return m, m.refreshPanel()
				}
				return m, nil
			}
			return m, nil
		}
		if m.page == "confirm" {
			if key == "enter" {
				args := []string{"install", "--host", m.host, "--preset", m.preset}
				if m.preset == "custom" {
					args = append(args, "--protocols", m.entry)
				}
				m.page = "busy"
				return m, m.command(args...)
			}
			return m, nil
		}
		items := m.choices()
		switch key {
		case "up", "k":
			m.cursor = (m.cursor - 1 + len(items)) % len(items)
		case "down", "j":
			m.cursor = (m.cursor + 1) % len(items)
		case "home":
			m.cursor = 0
		case "end":
			m.cursor = len(items) - 1
		case "enter":
			item := items[m.cursor]
			if cmd, ok := m.acceptTool(item); ok {
				return m, cmd
			}
			if m.page == "preset" {
				m.preset = item.action
				m.page = "host"
				m.entry = m.host
				return m, nil
			}
			m.home = m.cursor
			m.title = m.t(item.en, item.zh)
			switch item.action {
			case "quit":
				return m, tea.Quit
			case "install":
				m.page = "preset"
				m.cursor = 0
			default:
				m.page = "busy"
				return m, m.command(strings.Fields(item.action)...)
			}
		}
	}
	return m, nil
}
func appendEntry(s, text string) string {
	for _, r := range text {
		if r > 32 && r < 127 && len(s) < 255 {
			s += string(r)
		}
	}
	return s
}
func (m dashboard) capacity() int { return max(1, m.height-8-len(m.panelRows())) }
func (m dashboard) lines() []string {
	return strings.Split(ansi.Hardwrap(m.body, max(8, min(m.width-4, 100)), false), "\n")
}
func (m dashboard) View() tea.View {
	if v, ok := m.toolView(); ok {
		return v
	}
	w := max(10, min(m.width-4, 100))
	var b strings.Builder
	line := func(s string) { b.WriteString("  " + ansi.Truncate(s, w, "…") + "\n") }
	line(m.paint("1;36", "SingForge") + "   " + m.paint("2", Version))
	line(m.paint("2", strings.Repeat("─", w)))
	title := m.title
	if m.page == "home" {
		title = m.t("Server management", "服务器管理")
	}
	if m.page == "preset" {
		title = m.t("Deployment preset", "选择安装预设")
	}
	for _, row := range m.panelRows() {
		line(row)
	}
	line(m.paint("1", title))
	line("")
	hint := m.t("↑/↓ or number   Enter open   L 中文   q exit", "↑/↓ 或数字   Enter 打开   L English   q 退出")
	viewPage := m.page
	if strings.HasPrefix(viewPage, "cmd:") {
		viewPage = "catalog"
	}
	switch viewPage {
	case "catalog", "remove-options", "home", "preset", "subscriptions", "formats", "updates", "nodes", "preferred-edges":
		items := m.choices()
		limit := min(len(items), m.capacity())
		first := max(0, m.cursor-limit+1)
		for i := first; i < first+limit; i++ {
			name := m.t(items[i].en, items[i].zh)
			name = fmt.Sprintf("%2d.  %s", m.number(i), name)
			if i == m.cursor {
				line(m.paint("1;36", "❯ "+name))
			} else {
				line("  " + name)
			}
		}
		if m.page != "home" {
			hint = m.t("↑/↓ move   Enter select   Esc back", "↑/↓ 选择   Enter 确认   Esc 返回")
		}
	case "flag-value":
		line(m.paint("1", "--"+m.flagName))
		for _, r := range strings.Split(ansi.Hardwrap(m.flagHelp, w, false), "\n") {
			line(r)
		}
		line(m.paint("36", "❯ "+m.entry+"▏"))
		hint = m.t("Enter save   Esc cancel", "Enter 保存   Esc 取消")
	case "publication-form", "png-path", "renewal-credentials":
		label := m.t("New PNG file path", "新 PNG 文件路径")
		if m.page == "renewal-credentials" {
			label = m.t("Root-private Cloudflare DNS INI path", "Cloudflare DNS 凭据 INI 文件路径")
		}
		if m.page == "publication-form" {
			label = []string{m.t("Public HTTPS hostname", "公开订阅域名"), m.t("Certificate chain file", "证书链文件路径"), m.t("Private key file", "私钥文件路径")}[m.fieldIndex]
		}
		line(label)
		line(m.paint("36", "❯ "+m.entry+"▏"))
		if m.problem != "" {
			for _, r := range strings.Split(ansi.Hardwrap(m.problem, w, false), "\n") {
				line(r)
			}
		}
		hint = m.t("Enter continue   Esc back", "Enter 继续   Esc 返回")
	case "tool-confirm":
		prompt := m.t("Download, verify and apply this update?", "下载、校验并应用更新？")
		if m.pending[0] == "uninstall" {
			prompt = m.t("Confirm the selected local uninstall operation?", "确认执行所选本机卸载操作？")
			if slices.Contains(m.pending, "--full") || slices.Contains(m.pending, "--full=true") {
				prompt = m.t("Delete local services, credentials, caches and installed CLI?", "删除本机服务、凭据、缓存及 CLI 本身？")
			}
		} else if m.resultKind == "catalog" {
			prompt = m.t("Run this command?", "执行以下命令？")
		} else if m.resultKind == "issued" {
			prompt = m.t("Issue and reveal a private HTTPS subscription URL?", "签发并显示一个新的私密 HTTPS 订阅地址？")
		} else if m.pending[0] == "edge" {
			prompt = m.t("Apply this selector to full Mihomo subscriptions?", "将此优选策略应用到完整 Mihomo 订阅？")
		} else if m.pending[0] == "certificate" {
			prompt = m.t("Run Certbot DNS-01 automation?", "执行 Certbot DNS-01 自动续签操作？")
		} else if m.pending[0] == "subscription" {
			prompt = m.t("Enable public HTTPS subscriptions?", "启用公开 HTTPS 订阅？")
		}
		line(prompt)
		for _, r := range strings.Split(ansi.Hardwrap("singforge "+strings.Join(m.pending, " "), w, false), "\n") {
			line(r)
		}
		hint = m.t("Enter confirm   Esc cancel", "Enter 确认   Esc 取消")
	case "busy":
		line(m.t("Working…", "正在执行…"))
		hint = m.t("Ctrl+C exit", "Ctrl+C 退出")
	case "detail":
		rows := m.lines()
		start := min(m.scroll, max(0, len(rows)-m.capacity()))
		for _, r := range rows[start:min(start+m.capacity(), len(rows))] {
			line(r)
		}
		hint = m.t("↑/↓ scroll   Esc back", "↑/↓ 滚动   Esc 返回")
		if m.qr != "" {
			hint = m.t("r show QR   Esc close", "r 查看二维码   Esc 关闭")
		}
	case "host", "custom":
		label := m.t("Public IP or hostname", "公网 IP 或域名")
		if m.page == "custom" {
			label = m.t("Protocol IDs, separated by commas", "协议 ID，以逗号分隔")
			line(strings.Join(ids(), ", "))
		}
		line(label)
		line(m.paint("36", "❯ "+m.entry+"▏"))
		if m.problem != "" {
			line(m.paint("33", m.problem))
		}
		hint = m.t("Enter continue   Esc back", "Enter 继续   Esc 返回")
	case "confirm":
		line(m.t("Apply deployment?", "应用这次部署？"))
		line(m.host + "  ·  " + m.preset)
		hint = m.t("Enter apply   Esc back", "Enter 应用   Esc 返回")
	}
	line("")
	line(m.paint("2", hint))
	v := tea.NewView(strings.TrimRight(b.String(), "\n"))
	v.AltScreen = true
	return v
}
func menu(args []string, root *cobra.Command) error {
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, h, e := term.GetSize(int(os.Stdout.Fd()))
	if e != nil || w < 1 || h < 1 {
		w, h = 80, 24
	}
	_, e = tea.NewProgram(dashboard{commandMenu: commandMenu{root: root}, ctx: ctx, args: args, binary: exe, zh: chinese(), color: os.Getenv("NO_COLOR") == "", width: w, height: h, page: "home"}, tea.WithContext(ctx)).Run()
	return e
}
