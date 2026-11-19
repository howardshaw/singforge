package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/howardshaw/singforge/internal/edge"
	"github.com/howardshaw/singforge/internal/host"
	"github.com/howardshaw/singforge/internal/model"
)

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
	if op.options.dry {
		return printJSON(plan)
	}
	if c.Domain != "" {
		if !op.manager.Live {
			return errors.New("cloud provisioning requires live mode")
		}
		if err = host.ValidateCredentialFile(c.TokenFile); err != nil {
			return err
		}
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
		_, persist, err := cloudCommand(cmd, &s, op.manager, o)
		if err != nil {
			return err
		}
		if persist {
			return op.manager.Apply(s)
		}
		return nil
	}
	if err = cloud("cloudflare configure", args(map[string]string{"domain": c.Domain, "token-file": c.TokenFile})); err != nil {
		return err
	}
	syncDNS := func() error { return cloud("cloudflare dns sync", args(map[string]string{"address": c.IP})) }
	if err = syncDNS(); err != nil {
		return err
	}
	if c.Tunnel {
		o := args(nil)
		if s.TunnelCredentials == "" && s.ConnectorToken == "" {
			ledger := filepath.Join(op.manager.Dir, "created-tunnel.json")
			data, readErr := os.ReadFile(ledger)
			switch {
			case os.IsNotExist(readErr):
				if err = cloud("cloudflare tunnel create", args(map[string]string{"name": c.hostname("edge")})); err != nil {
					return err
				}
			case readErr != nil:
				return readErr
			default:
				var saved struct{ AccountTag string }
				if json.Unmarshal(data, &saved) != nil || saved.AccountTag != s.AccountID {
					return errors.New("saved tunnel belongs to a different account")
				}
				o = args(map[string]string{"credentials": ledger})
			}
		}
		if err = Edge(&s, "tunnel", c.hostname("edge"), o); err != nil {
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
		if err = certbotRun("certonly", "--non-interactive", "--agree-tos", "--email", c.Email, "--keep-until-expiring", "--cert-name", s.PublishHost, "--dns-cloudflare", "--dns-cloudflare-credentials", c.CertFile, "--dns-cloudflare-propagation-seconds", "60", "-d", s.PublishHost); err != nil {
			return err
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
		if err = op.autoRenew(); err != nil {
			return fmt.Errorf("enable renewal: %w", err)
		}
	}
	return printJSON(plan)
}
