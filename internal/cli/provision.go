package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/howardshaw/singforge/internal/cloudflare"
	"github.com/howardshaw/singforge/internal/edge"
	"github.com/howardshaw/singforge/internal/host"
	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/store"
)

// Report only operator-facing stage names. Keep stdout available for the JSON plan.
func provisionProgress(out io.Writer, interval time.Duration) (report func(string), stop func()) {
	stages := make(chan string)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var stage string
		var since time.Time
		for {
			select {
			case next, ok := <-stages:
				if !ok {
					return
				}
				stage, since = next, time.Now()
				fmt.Fprintf(out, "[provision] %s\n", stage)
				ticker.Reset(interval)
			case <-ticker.C:
				if stage != "" {
					fmt.Fprintf(out, "[provision] %s (%s elapsed)\n", stage, time.Since(since).Round(time.Second))
				}
			}
		}
	}()
	return func(s string) { stages <- s }, func() { close(stages); <-done }
}

// All milestones run under the same deployment lock. Persist the tunnel ID
// before activation so a retry uses the existing remote resource.
func (op operation) provision() error {
	c, err := readProvision(op.options.get("env-file"))
	if err != nil {
		return err
	}
	direct := c.IP
	if c.Domain != "" {
		direct = c.hostname("fast")
	}
	s := op.state
	if op.installed {
		if s.Direct != direct && s.Direct != c.IP || s.Tunnel != "" && (!c.Tunnel || s.Tunnel != c.hostname("edge")) || s.PublishHost != "" && (!c.Publish || s.PublishHost != c.hostname("sub")) || s.Zone != "" && s.Zone != c.Domain {
			return errors.New("env file conflicts with this installation; use explicit endpoint commands to migrate")
		}
		for i := range s.Endpoints {
			if s.Endpoints[i].Mode == "direct" && s.Endpoints[i].Host == s.Direct {
				s.Endpoints[i].Host = direct
			}
		}
		s.Direct = direct
	} else {
		state, err := model.New(direct)
		if err != nil {
			return err
		}
		s = *state
		if netip.MustParseAddr(c.IP).Is6() {
			s.ServerDNSFamily = "ipv6"
			s.DNSFamily = "ipv6"
		}
	}
	for _, p := range []struct {
		id   string
		port int
	}{{"reality", c.Reality}, {"hysteria2", c.HY2}, {"ss", c.SS}} {
		if err := Direct(&s, p.id, p.port); err != nil {
			return err
		}
	}
	if c.NodeDNS != "" {
		s.MihomoNodeDNS = c.NodeDNS
	}
	if err := s.Validate(); err != nil {
		return err
	}
	plan := map[string]any{"direct": direct, "ip": c.IP, "reality_tcp": c.Reality, "hysteria2_udp": c.HY2, "shadowsocks_tcp_udp": c.SS}
	if c.Tunnel {
		plan["tunnel"] = c.hostname("edge")
	}
	if c.Publish {
		plan["subscription"] = c.hostname("sub")
	}
	if op.options.dry && !op.manager.Live {
		return printJSON(plan)
	}
	progress := op.manager.Progress
	if progress == nil {
		progress = func(string) {}
	}
	progress("Checking provision prerequisites")
	var token []byte
	var recoverID string
	var client cloudflare.Client
	if c.Domain != "" {
		if !op.manager.Live {
			return errors.New("cloud provisioning requires live mode")
		}
		if err = host.ValidateCredentialFile(c.TokenFile); err != nil {
			return err
		}
		token, err = os.ReadFile(c.TokenFile)
		if err != nil {
			return err
		}
		progress("Checking Cloudflare zone, tunnel and DNS records")
		client = cloudflare.New(strings.TrimSpace(string(token)))
		if recoverID, err = checkProvisionCloud(client, c, &s, op.manager.Dir); err != nil {
			return err
		}
		if recoverID != "" {
			plan["recover_tunnel_id"] = recoverID
		}
	}
	if op.options.dry {
		return printJSON(plan)
	}
	if c.Publish {
		if err = host.ValidateDNSCredentials(c.CertFile); err != nil {
			return err
		}
		for _, name := range []string{"certbot", "nginx"} {
			if _, err = exec.LookPath(name); err != nil {
				return err
			}
		}
	}
	if recoverID != "" {
		progress("Recovering existing Cloudflare tunnel " + recoverID)
		s.TunnelCredentials, err = client.RecoverLocalCredentials(s.AccountID, c.hostname("edge"), recoverID)
		if err != nil {
			return err
		}
		if err = store.Write(filepath.Join(op.manager.Dir, "created-tunnel.json"), []byte(s.TunnelCredentials), 0600); err != nil {
			return err
		}
		s.TunnelID = recoverID
	}
	progress("Applying direct endpoints")
	if err = op.manager.Apply(s); err != nil {
		return err
	}
	if c.Domain == "" {
		return printJSON(plan)
	}
	args := func(values map[string]string) opts {
		o := opts{yes: true, quiet: true, str: map[string]*string{}}
		for k, v := range values {
			o.str[k] = &v
		}
		return o
	}
	cloud := func(cmd string, o opts) error {
		progress(cmd)
		_, persist, err := cloudCommand(cmd, &s, op.manager, o)
		if err != nil {
			return err
		}
		if persist {
			return op.manager.Apply(s)
		}
		return nil
	}
	if err = store.Write(filepath.Join(op.manager.Dir, "cloudflare.token"), token, 0600); err != nil {
		return err
	}
	syncDNS := func() error { return cloud("cloudflare dns sync", args(map[string]string{"address": c.IP})) }
	if err = syncDNS(); err != nil {
		return err
	}
	if c.Tunnel {
		if s.TunnelCredentials == "" && s.ConnectorToken == "" {
			if err = cloud("cloudflare tunnel create", args(map[string]string{"name": c.hostname("edge")})); err != nil {
				return err
			}
		}
		progress("Configuring tunnel endpoints")
		if err = Edge(&s, "tunnel", c.hostname("edge"), args(nil)); err != nil {
			return err
		}
		s.PreferredEdge = model.EdgePreferences{Mode: "all", Addresses: append(append([]string{}, edge.Community...), edge.DefaultIPs...)}
		if err = op.manager.Apply(s); err != nil {
			return err
		}
		if err = syncDNS(); err != nil {
			return err
		}
	}
	if c.Publish {
		s.PublishHost = c.hostname("sub")
		progress("Requesting HTTPS certificate (DNS-01 propagation may take 60s)")
		output, err := certbotOutput("certonly", "--non-interactive", "--agree-tos", "--email", c.Email, "--keep-until-expiring", "--cert-name", s.ActiveCertbotName(), "--dns-cloudflare", "--dns-cloudflare-credentials", c.CertFile, "--dns-cloudflare-propagation-seconds", "60", "-d", s.PublishHost)
		if err != nil {
			return err
		}
		name, err := issuedCertbotName(s, output)
		if err != nil {
			return err
		}
		if name == s.PublishHost {
			s.CertbotName = ""
		} else {
			s.CertbotName = name
		}
		s.PublishCert, s.PublishKey, err = readRenewed(s)
		if err != nil {
			return err
		}
		s.PublishCA = ""
		if err = op.manager.Apply(s); err != nil {
			return err
		}
		if err = syncDNS(); err != nil {
			return err
		}
		op.state = s
		op.path = "certificate auto-renew enable"
		op.options = args(map[string]string{"credentials": c.CertFile})
		progress("Enabling and checking certificate renewal")
		if err = op.autoRenew(); err != nil {
			return fmt.Errorf("enable renewal: %w", err)
		}
	}
	return printJSON(plan)
}

// Check every enabled hostname before applying endpoints or issuing DNS writes.
func checkProvisionCloud(client cloudflare.Client, c provisionConfig, s *model.State, dir string) (string, error) {
	z, err := client.Zone(c.Domain)
	if err != nil {
		return "", err
	}
	if (s.ZoneID != "" && s.ZoneID != z.ID) || (s.AccountID != "" && s.AccountID != z.Account.ID) {
		return "", errors.New("configured Cloudflare zone or account differs from this installation")
	}
	s.Zone, s.ZoneID, s.AccountID = z.Name, z.ID, z.Account.ID
	var tunnelErr error
	var recoverID string
	target := *s
	if c.Tunnel {
		if err := loadProvisionTunnel(s, dir); err != nil {
			return "", err
		}
		target.Tunnel = c.hostname("edge")
		target.TunnelID, tunnelErr = client.CheckTunnel(s.AccountID, target.Tunnel, s.TunnelID)
		var conflict *cloudflare.TunnelNameConflict
		if s.TunnelID == "" && errors.As(tunnelErr, &conflict) && conflict.Existing.Name == target.Tunnel && conflict.Existing.Recoverable() {
			recoverID, tunnelErr = conflict.Existing.ID, nil
		}
	}
	if c.Publish {
		target.PublishHost = c.hostname("sub")
	}
	dnsErr := syncCloudDNS(client, target, dir, opts{dry: true, quiet: true, str: map[string]*string{"address": &c.IP}})
	if err := errors.Join(tunnelErr, dnsErr); err != nil {
		return "", err
	}
	return recoverID, nil
}

func loadProvisionTunnel(s *model.State, dir string) error {
	if s.ConnectorToken != "" {
		id, err := host.ConnectorID(s.ConnectorToken)
		if err != nil {
			return err
		}
		if s.TunnelID != "" && s.TunnelID != id {
			return errors.New("connector token differs from configured tunnel ID")
		}
		s.TunnelID = id
		return nil
	}
	if s.TunnelCredentials == "" {
		data, err := os.ReadFile(filepath.Join(dir, "created-tunnel.json"))
		if os.IsNotExist(err) && s.TunnelID == "" {
			return nil
		}
		if err != nil {
			return err
		}
		s.TunnelCredentials = string(data)
	}
	var saved struct{ AccountTag string }
	if json.Unmarshal([]byte(s.TunnelCredentials), &saved) != nil || saved.AccountTag != s.AccountID {
		return errors.New("saved tunnel belongs to a different account or has invalid credentials")
	}
	id, err := host.TunnelID(s.TunnelCredentials)
	if err != nil {
		return err
	}
	if s.TunnelID != "" && s.TunnelID != id {
		return errors.New("saved credentials differ from configured tunnel ID")
	}
	s.TunnelID = id
	return nil
}
