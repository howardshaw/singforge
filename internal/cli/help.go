package cli

import (
	"github.com/spf13/cobra"
	"strings"
)

func groupHelp(root *cobra.Command) {
	groups := []struct{ id, en, zh, commands string }{
		{"operate", "Operate the server", "服务运行", "status doctor start stop restart reload logs"},
		{"connect", "Configure connectivity", "协议与接入", "protocol reality endpoint domain cloudflare dns rules certificate firewall"},
		{"distribute", "Distribute client configurations", "客户端分发", "subscription"},
		{"maintain", "Install and maintain", "安装与维护", "install backup bbr cloudflared core self update uninstall"},
		{"reference", "Reference", "参考", "version completion help"},
	}
	for _, g := range groups {
		title := g.en
		if chinese() {
			title = g.zh
		}
		root.AddGroup(&cobra.Group{ID: g.id, Title: title})
		for _, name := range strings.Fields(g.commands) {
			for _, c := range root.Commands() {
				if c.Name() == name {
					c.GroupID = g.id
				}
			}
		}
	}
	root.SetCompletionCommandGroupID("reference")
	root.SetHelpCommandGroupID("reference")
	summaries := map[string]string{"backup": "Create and restore private backups", "bbr": "Inspect and tune kernel congestion control", "certificate": "Import certificates or renew client pins", "cloudflare": "Manage Cloudflare DNS and named tunnels", "cloudflared": "Update or roll back the tunnel connector", "core": "Update or roll back sing-box", "dns": "Configure and test client DNS", "domain": "Configure public hostnames and DNS records", "endpoint": "Inspect and configure public access points", "firewall": "Synchronize optional IPv4 ingress rules", "protocol": "Configure server protocol listeners", "reality": "Configure the Reality TLS handshake", "rules": "Manage client routing policies and rule data", "self": "Update or roll back the SingForge executable", "subscription": "Export client profiles and manage HTTPS publication"}
	for _, c := range root.Commands() {
		if s := summaries[c.Name()]; s != "" {
			c.Short = s
		}
	}
}
