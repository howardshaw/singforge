package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"

	"github.com/howardshaw/singforge/internal/host"
	"github.com/howardshaw/singforge/internal/subscription"
)

var Version = "dev"

type commandSpec struct {
	path, summary, flags string
	run                  func(operation) error
}

var commands = []commandSpec{
	{"edge status", "Show the Cloudflare baselines and preferred dial addresses", "", operation.preferredEdge},
	{"edge set", "Publish Cloudflare endpoint variants as selectable Mihomo nodes", "mode! domains ips", operation.preferredEdge},
	{"certificate auto-renew enable", "Configure Cloudflare DNS-01 and deploy renewed subscription certificates", "credentials!", operation.autoRenew},
	{"certificate auto-renew status", "Inspect the Certbot timer, authenticator, hook and served leaf", "", operation.autoRenew},
	{"certificate auto-renew test", "Exercise a staging renewal and verify the live HTTPS origin", "", operation.autoRenew},
	{"certificate auto-renew sync", "Import the current Certbot certificate and verify HTTPS", "", operation.autoRenew},
	{"reality configure", "Set the Reality server name and TLS handshake destination", "handshake! handshake-target!", operation.realityConfigure},
	{"firewall sync", "Reconcile optional IPv4 iptables-persistent ingress", "", operation.firewallSync},
	{"update", "Update all enabled components from stable upstream releases", "all", operation.updateAll},
	{"certificate import", "Import a publicly trusted direct TLS certificate and key", "host! cert! key!", operation.certificateImport}, {"certificate renew", "Generate a new direct certificate and new client pins", "", operation.certificateRenew},
	{"bbr status", "Inspect kernel congestion control and queue settings", "", operation.bbrStatus},
	{"bbr enable", "Plan or apply reversible TCP tuning", "profile", operation.bbrStatus},
	{"bbr disable", "Restore recorded kernel values", "", operation.bbrStatus},
	{"cloudflared update", "Stage a verified connector and test tunnel readiness", "file sha256", operation.cloudflaredUpdate},
	{"cloudflared rollback", "Restore the previous connector", "", operation.cloudflaredRollback},
	{"core update", "Stage a digest-verified core, validate and activate", "file sha256", operation.coreUpdate},
	{"core rollback", "Reapply the previous core with current credentials", "", operation.coreRollback},
	{"self update", "Replace the CLI from a digest-verified executable", "file sha256", operation.selfUpdate},
	{"self rollback", "Restore the preceding CLI executable", "", operation.selfUpdate},
	{"install", "Deploy a validated generation and start managed services", "config host preset mode protocols listen handshake handshake-target cdn tunnel credentials connector-token-file cert key port server-family", operation.install},
	{"status", "Show deployment status without credentials", "", operation.status},
	{"doctor", "Check the active server configuration and service health", "", operation.doctor},
	{"start", "Start managed services", "", operation.start},
	{"stop", "Stop managed services", "", operation.start},
	{"restart", "Restart managed services", "", operation.start},
	{"reload", "Reload or restart managed services", "", operation.start},
	{"logs", "Read recent managed service journal entries", "", operation.start},
	{"protocol list", "List protocol capabilities", "", operation.protocolList},
	{"protocol enable", "Enable a protocol and its endpoints", "id! port", operation.protocolEnable},
	{"protocol disable", "Disable an inbound without deleting credentials", "id!", operation.protocolConfigure},
	{"protocol configure", "Change the listening port", "id! port!", operation.protocolConfigure},
	{"endpoint list", "List public endpoints", "", operation.endpointList},
	{"endpoint add", "Add or update a public endpoint", "id! mode host! port name", operation.endpointAdd},
	{"endpoint remove", "Remove a public endpoint", "id!", operation.endpointRemove},
	{"endpoint test", "Test TCP reachability", "id!", operation.endpointTest},
	{"subscription list", "Inspect publication and grant counts", "", operation.subscriptionList},
	{"subscription show", "Reveal client credentials or export a private file", "format filter output qr", operation.subscriptionShow},
	{"subscription publish", "Publish subscriptions over HTTPS", "host! cert! key! ca", operation.subscriptionPublish},
	{"subscription unpublish", "Stop publication and revoke grants", "", operation.subscriptionRevoke},
	{"subscription nodes", "List enabled endpoints with a compatible import URI", "", operation.subscriptionNodes},
	{"subscription issue", "Issue another private HTTPS URL, preserving previous grants", "ttl format", operation.subscriptionRotate},
	{"subscription rotate", "Issue a bearer URL, invalidating previous grants after grace", "ttl grace format", operation.subscriptionRotate},
	{"subscription revoke", "Invalidate all subscription URLs", "", operation.subscriptionRevoke},
	{"domain configure", "Set a DNS-only direct hostname", "domain! hostname! mode!", operation.domainConfigure},
	{"domain status", "Inspect configured hostnames", "", operation.domainStatus},
	{"domain sync", "Reconcile configured DNS records", "address replace-id", operation.cloudAPI},
	{"cloudflare configure", "Discover an existing zone using a scoped token", "domain! token-file!", operation.cloudAPI},
	{"cloudflare enable", "Configure HTTP CDN or named tunnel access", "mode! host! port credentials connector-token-file cert key", operation.cloudflareEnable},
	{"cloudflare disable", "Remove an access mode", "mode!", operation.cloudflareDisable},
	{"cloudflare status", "Show configured edge hosts", "", operation.domainStatus},
	{"cloudflare dns cleanup", "Delete records created in the selected zone", "", operation.cloudAPI},
	{"cloudflare dns list", "List records in the selected zone", "", operation.cloudAPI},
	{"cloudflare dns sync", "Check conflicts and synchronize managed DNS", "address replace-id", operation.cloudAPI},
	{"cloudflare tunnel create", "Provision a locally managed named tunnel", "name!", operation.cloudAPI},
	{"cloudflare tunnel routes", "Show the required public hostname, path patterns and origins", "", operation.tunnelRoutes},
	{"cloudflare tunnel list", "Discover tunnels in the configured account", "", operation.cloudAPI},
	{"cloudflare tunnel status", "Inspect tunnel connection status", "", operation.tunnelStatus},
	{"dns test", "Query a running local client DNS listener", "host! port", operation.dnsTest},
	{"dns show", "Inspect client DNS settings", "", operation.dnsShow},
	{"dns set", "Change client DNS profile or encrypted resolver", "profile resolver transport capture family server-family mihomo-policy mihomo-ipv6 mihomo-node-dns", operation.dnsSet},
	{"rules schedule", "Enable the weekly systemd rule refresh timer", "", operation.rulesSchedule}, {"rules unschedule", "Disable the rule refresh timer", "", operation.rulesSchedule},
	{"rules update", "Download and verify pinned routing data", "", operation.rulesUpdate},
	{"rules validate", "Validate cached rules and overrides", "", operation.rulesValidate},
	{"rules override", "Prepend a domain or CIDR routing override", "kind! value! group!", operation.rulesOverride},
	{"rules rollback", "Swap cached rules with the previous revision", "", operation.rulesRollback},
	{"rules list", "List built-in routing rules", "", operation.rulesList},
	{"rules test", "Explain routing for a hostname", "host!", operation.rulesTest},
	{"rules set", "Select a routing preset", "profile!", operation.rulesSet},
	{"backup create", "Save an integrity-checked private backup", "file!", operation.backupCreate},
	{"backup restore", "Validate and activate a backup", "file!", operation.backupRestore},
	{"uninstall", "Detach managed services and optionally purge state", "purge", operation.uninstall},
}
var defaults = map[string]string{"listen": "::", "preset": "recommended", "mode": "direct", "format": "mihomo", "handshake": "www.microsoft.com"}
var descriptions = map[string]string{"ips": "Ordered comma-separated Cloudflare IPv4 or IPv6 addresses", "domains": "Ordered comma-separated preferred hostnames (defaults to the community list)", "mihomo-node-dns": "Resolver for exported node hostnames: default or alidns (direct encrypted DNS)", "mihomo-ipv6": "Enable or disable client IPv6 in full Mihomo exports: on or off", "mihomo-policy": "Mihomo DNS policy: strict or split (exact domestic host allowlist)", "connector-token-file": "File containing a dashboard-managed tunnel connector token", "handshake-target": "Reality handshake destination host:port (defaults to the server name on TCP 443)", "config": "Installation YAML manifest; explicit flags override fields", "server-family": "Server-side encrypted resolver address family: ipv4 or ipv6", "ca": "Private CA already trusted by all subscription clients", "family": "Resolver IP family: ipv4 or ipv6", "transport": "Encrypted DNS transport: https or tls", "capture": "Client capture mode: system or tun", "sha256": "Trusted lowercase SHA-256 digest of the binary", "kind": "DOMAIN, DOMAIN-SUFFIX, IP-CIDR or IP-CIDR6", "value": "Match domain or CIDR", "group": "Main, AI, Streaming, Communication, DIRECT or REJECT", "listen": "Local IPv4 or IPv6 bind address", "host": "Public hostname or unbracketed IP address", "preset": "minimal, recommended, full or custom", "protocols": "Comma-separated protocol IDs for custom preset", "mode": "Connectivity mode for this operation", "handshake": "TLS 1.3 hostname for Reality and ShadowTLS", "cdn": "Hybrid CDN hostname", "tunnel": "Hybrid named tunnel hostname", "cert": "PEM certificate chain file", "key": "PEM private key file", "credentials": "Credentials file (Certbot Cloudflare INI for auto-renew, JSON for local tunnels)", "domain": "Cloudflare zone name", "hostname": "Direct hostname relative to the zone", "format": "mihomo, provider, v2ray, sing-box, shadowsocks or uri", "filter": "Select one endpoint ID or access mode", "output": "Create a private output file; with --qr, write PNG", "address": "Comma-separated public IPv4 and IPv6 addresses", "replace-id": "Confirm modification of this existing DNS record ID", "token-file": "File containing a scoped Cloudflare API token", "resolver": "cloudflare, google or quad9", "id": "Protocol or endpoint identifier", "profile": "Configuration profile", "file": "Backup file path", "name": "Named tunnel name"}

func NewCommand() *cobra.Command {
	root := &cobra.Command{Use: "singforge", Short: "Deploy and manage a single-host sing-box server", Version: Version + " (sing-box " + host.CoreVersion + ")", SilenceUsage: true, SilenceErrors: true, Args: cobra.NoArgs}
	root.RunE = func(c *cobra.Command, _ []string) error {
		if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("TERM") != "dumb" {
			var options []string
			c.PersistentFlags().Visit(func(f *pflag.Flag) { options = append(options, "--"+f.Name+"="+f.Value.String()) })
			return menu(options)
		}
		return c.Help()
	}
	persistent := root.PersistentFlags()
	persistent.String("workspace", "", "Use a private local deployment workspace")
	persistent.String("core", "", "Path to the official core for workspace validation")
	persistent.Bool("dry-run", false, "Preview changes")
	persistent.Bool("yes", false, "Confirm destructive operations")
	persistent.Bool("json", false, "Request machine-readable output")
	tree := map[string]*cobra.Command{"": root}
	for _, spec := range commands {
		parent := ""
		for _, part := range strings.Fields(spec.path) {
			path := strings.TrimSpace(parent + " " + part)
			if tree[path] == nil {
				c := &cobra.Command{Use: part, Short: "Manage " + part, Args: cobra.NoArgs}
				c.RunE = func(c *cobra.Command, _ []string) error { return c.Help() }
				tree[parent].AddCommand(c)
				tree[path] = c
			}
			parent = path
		}
		leaf := tree[parent]
		leaf.Short = spec.summary
		leaf.RunE = func(c *cobra.Command, _ []string) error {
			if file, _ := c.Flags().GetString("config"); file != "" {
				if e := loadManifest(file, c.Flags()); e != nil {
					return e
				}
			}
			return execute(spec.path, options(c.Flags()), spec.run)
		}
		for _, key := range strings.Fields(spec.flags) {
			name := strings.TrimSuffix(key, "!")
			f := leaf.Flags()
			switch name {
			case "qr", "purge", "all":
				f.Bool(name, false, map[string]string{"all": "Update core, CLI, rules and enabled connector", "qr": "Render a terminal QR or PNG", "purge": "Remove managed private state"}[name])
			case "port":
				f.Int(name, 0, "Port number")
			case "ttl", "grace":
				f.Duration(name, 0, map[string]string{"ttl": "New grant lifetime (0 means unlimited)", "grace": "Previous grant overlap (for example 15m)"}[name])
			default:
				if spec.path == "edge set" && name == "mode" {
					f.String(name, "", "Select default, domains, ips or all")
				} else {
					f.String(name, defaults[name], descriptions[name])
				}
			}
			if key != name {
				_ = leaf.MarkFlagRequired(name)
			}
		}
	}

	root.AddCommand(&cobra.Command{Use: "version", Short: "Print compatible core version", Args: cobra.NoArgs, Run: func(c *cobra.Command, _ []string) { c.Print(c.Root().Version + "\n") }})
	root.AddCommand(&cobra.Command{Use: "serve BUNDLE", Hidden: true, Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, a []string) error { return subscription.Serve(a[0]) }})
	groupHelp(root)
	localize(root)
	return root
}
func options(f *pflag.FlagSet) opts {
	o := opts{str: map[string]*string{}}
	f.VisitAll(func(v *pflag.Flag) {
		if v.Value.Type() == "string" {
			d := v.Value.String()
			o.str[v.Name] = &d
		}
	})
	o.yes, _ = f.GetBool("yes")
	o.dry, _ = f.GetBool("dry-run")
	o.json, _ = f.GetBool("json")
	o.all, _ = f.GetBool("all")
	o.qr, _ = f.GetBool("qr")
	o.purge, _ = f.GetBool("purge")
	o.port, _ = f.GetInt("port")
	o.ttl, _ = f.GetDuration("ttl")
	o.grace, _ = f.GetDuration("grace")
	return o
}
func Run(args []string) error { c := NewCommand(); c.SetArgs(args); return c.Execute() }
