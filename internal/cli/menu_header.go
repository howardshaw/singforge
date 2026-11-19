package cli

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"fmt"
	"github.com/howardshaw/singforge/internal/host"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

type hostPanel struct {
	System, Network string
	Services        []string
}

func readPanel(ctx context.Context, workspace bool) hostPanel {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	local := func(name string, args ...string) string {
		b, _ := exec.CommandContext(ctx, name, args...).Output()
		return cleanText(strings.TrimSpace(string(b)))
	}
	osName := runtime.GOOS
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for l := range strings.SplitSeq(string(b), "\n") {
			if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
				osName = strings.Trim(v, "\"")
				break
			}
		}
	}
	p := hostPanel{System: osName + "  ·  " + local("uname", "-r") + "  ·  " + runtime.GOARCH}
	if runtime.GOOS != "linux" || workspace {
		return p
	}
	if virt := local("systemd-detect-virt"); virt != "" && virt != "none" {
		p.System += "  ·  " + virt
	}
	var ips []string
	if addrs, e := net.InterfaceAddrs(); e == nil {
		for _, a := range addrs {
			ip, _, e := net.ParseCIDR(a.String())
			if e == nil && ip.IsGlobalUnicast() && !ip.IsPrivate() {
				ips = append(ips, ip.String())
			}
		}
	}
	if len(ips) > 0 {
		p.Network = strings.Join(ips, "  ")
	}
	units := []string{"core", "tunnel", "edge", "subscription"}
	labels := []string{"sing-box", "cloudflared", "Nginx", "Subscriptions"}
	args := []string{"show", "-p", "Id,LoadState,ActiveState,MemoryCurrent,MainPID,IPIngressBytes,IPEgressBytes"}
	for _, u := range units {
		args = append(args, "singforge-"+u+".service")
	}
	states := map[string]string{}
	for block := range strings.SplitSeq(local("systemctl", args...), "\n\n") {
		fields := map[string]string{}
		for l := range strings.SplitSeq(block, "\n") {
			k, v, _ := strings.Cut(l, "=")
			fields[k] = v
		}
		state := fields["ActiveState"]
		if fields["LoadState"] == "not-found" {
			state = "—"
		}
		if state == "active" {
			version := ""
			pid, e := strconv.Atoi(fields["MainPID"])
			if e == nil && pid > 1 {
				exe, _ := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
				switch {
				case fields["Id"] == "singforge-core.service" && filepath.Dir(exe) == host.Bin:
					v := strings.Fields(local(exe, "version"))
					if len(v) >= 3 && v[0] == "sing-box" {
						version = v[2]
					}
				case fields["Id"] == "singforge-tunnel.service" && filepath.Dir(exe) == host.Bin:
					v := strings.Fields(local(exe, "--version"))
					if len(v) >= 3 && v[0] == "cloudflared" {
						version = v[2]
					}
				case fields["Id"] == "singforge-subscription.service" && filepath.Dir(exe) == host.Bin:
					version = Version
				case fields["Id"] == "singforge-edge.service" && exe == "/usr/sbin/nginx":
					b, _ := exec.CommandContext(ctx, exe, "-v").CombinedOutput()
					_, version, _ = strings.Cut(strings.TrimSpace(string(b)), "nginx/")
				}
			}
			state = fmt.Sprintf("%-8s %-15s", state, version)
			if mem, e := strconv.ParseUint(fields["MemoryCurrent"], 10, 64); e == nil && mem < 1<<60 {
				state += fmt.Sprintf(" %5.1f MiB", float64(mem)/(1<<20))
			}
			for i, key := range []string{"IPIngressBytes", "IPEgressBytes"} {
				if v, e := strconv.ParseUint(fields[key], 10, 64); e == nil && v < 1<<60 {
					state += fmt.Sprintf("  %s%.1f MiB", []string{"↓", "↑"}[i], float64(v)/(1<<20))
				}
			}
		}
		states[fields["Id"]] = state
	}
	for i, u := range units {
		v := states["singforge-"+u+".service"]
		if v == "" {
			v = "unknown"
		}
		p.Services = append(p.Services, fmt.Sprintf("%-14s %s", labels[i], v))
	}
	return p
}
func (m dashboard) refreshPanel() tea.Cmd {
	return func() tea.Msg {
		return readPanel(m.ctx, slices.ContainsFunc(m.args, func(s string) bool { return strings.HasPrefix(s, "--workspace=") }))
	}
}
func (m dashboard) listPage() bool {
	if m.page == "home" || m.page == "preset" || m.page == "nodes" || strings.HasPrefix(m.page, "cmd:") {
		return true
	}
	_, ok := toolMenus[m.page]
	return ok
}
func (m dashboard) number(i int) int {
	if m.page == "home" && i == len(homeChoices)-1 {
		return 0
	}
	return i + 1
}
func (m dashboard) panelRows() []string {
	if m.page != "home" || m.panel.System == "" {
		return nil
	}
	rows := []string{m.panel.System}
	if m.panel.Network != "" {
		rows = append(rows, m.panel.Network)
	}
	if m.height >= 22 {
		rows = append(rows, m.panel.Services...)
	} else if len(m.panel.Services) > 0 {
		rows = append(rows, strings.TrimSpace(m.panel.Services[0]))
	}
	return rows
}
