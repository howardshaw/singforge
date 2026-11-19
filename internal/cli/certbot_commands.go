package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/howardshaw/singforge/internal/host"
	"github.com/howardshaw/singforge/internal/model"
)

func certbotRun(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "certbot", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("certbot %s failed: %w (see /var/log/letsencrypt/letsencrypt.log)", args[0], err)
	}
	return nil
}
func readRenewed(s model.State) (string, string, error) {
	if !model.Domain(s.PublishHost) {
		return "", "", fmt.Errorf("publish an HTTPS subscription first")
	}
	dir := host.Lineage(s.PublishHost)
	cert, e := read(filepath.Join(dir, "fullchain.pem"))
	if e != nil {
		return "", "", e
	}
	key, e := read(filepath.Join(dir, "privkey.pem"))
	if e != nil {
		return "", "", e
	}
	return cert, key, host.CheckTrust(s.PublishHost, cert, key, "")
}
func (op operation) autoRenew() error {
	if !op.manager.Live {
		return fmt.Errorf("run Certbot automation on the installed Linux host")
	}
	s := op.state
	name := s.PublishHost
	if op.path == "certificate auto-renew sync" && os.Getenv("RENEWED_LINEAGE") != "" && (!s.AutoRenew || os.Getenv("RENEWED_LINEAGE") != host.Lineage(name)) {
		return nil
	}
	if !model.Domain(name) {
		return fmt.Errorf("publish an HTTPS subscription first")
	}
	params, e := host.RenewalParams(name)
	if e != nil {
		return fmt.Errorf("no readable Certbot renewal configuration for %s: %w", name, e)
	}
	if op.path == "certificate auto-renew status" {
		cert, _, err := readRenewed(s)
		match := err == nil && host.VerifyServedCert(name, cert) == nil
		timer := exec.Command("systemctl", "is-active", "--quiet", "certbot.timer").Run() == nil
		boot := exec.Command("systemctl", "is-enabled", "--quiet", "certbot.timer").Run() == nil
		hook := host.RenewalHookInstalled(host.RenewalHook)
		credOK := host.ValidateDNSCredentials(params["dns_cloudflare_credentials"]) == nil
		return printJSON(map[string]any{"host": name, "authenticator": params["authenticator"], "timer_active": timer, "timer_enabled": boot, "deploy_hook_installed": hook, "serving_live_certificate": match, "dns_credentials_private": credOK, "configured": s.AutoRenew && hook && timer && boot && params["authenticator"] == "dns-cloudflare" && credOK})
	}
	cert, key, e := readRenewed(s)
	if e != nil {
		return e
	}
	switch op.path {
	case "certificate auto-renew enable":
		path := op.options.get("credentials")
		if e = host.ValidateDNSCredentials(path); e != nil {
			return e
		}
		if _, e = os.Stat("/usr/local/bin/singforge"); e != nil {
			return fmt.Errorf("deploy hook requires /usr/local/bin/singforge")
		}
		if _, e = os.Lstat(host.RenewalHook); e == nil && !host.RenewalHookInstalled(host.RenewalHook) {
			return fmt.Errorf("conflicting deploy hook at %s", host.RenewalHook)
		} else if e != nil && !os.IsNotExist(e) {
			return e
		}
		if op.options.dry {
			return printJSON(map[string]string{"cert_name": name, "authenticator": "dns-cloudflare", "timer": "certbot.timer", "hook": host.RenewalHook})
		}
		if !op.options.yes {
			return fmt.Errorf("use --yes to configure the Certbot renewal method")
		}
		if params["authenticator"] != "dns-cloudflare" || params["dns_cloudflare_credentials"] != path || params["dns_cloudflare_propagation_seconds"] != "60" {
			fmt.Fprintln(os.Stderr, "Testing DNS-01 with the staging CA (includes a 60-second DNS propagation wait)…")
			if e = certbotRun("reconfigure", "--cert-name", name, "--authenticator", "dns-cloudflare", "--dns-cloudflare-credentials", path, "--dns-cloudflare-propagation-seconds", "60", "--non-interactive"); e != nil {
				return e
			}
		}
		if e = host.InstallRenewalHook(host.RenewalHook); e != nil {
			return e
		}
		if e = exec.Command("systemctl", "enable", "--now", "certbot.timer").Run(); e != nil {
			return fmt.Errorf("enable certbot.timer: %w", e)
		}
	case "certificate auto-renew test":
		if !s.AutoRenew || params["authenticator"] != "dns-cloudflare" || !host.RenewalHookInstalled(host.RenewalHook) {
			return fmt.Errorf("enable DNS-01 auto-renewal first")
		}
		if op.options.dry {
			return printJSON(map[string]string{"staging_renewal": name})
		}
		// Do not run deploy hooks inside this subprocess: this operation holds the deployment lock.
		if e = certbotRun("renew", "--cert-name", name, "--dry-run", "--no-random-sleep-on-renew", "--non-interactive"); e != nil {
			return e
		}
	case "certificate auto-renew sync":
		if !s.AutoRenew {
			return fmt.Errorf("automatic deployment is not enabled")
		}
		if op.options.dry {
			return printJSON(map[string]bool{"needs_import": s.PublishCert != cert || s.PublishKey != key})
		}
	}
	if op.path != "certificate auto-renew sync" {
		cert, key, e = readRenewed(s)
		if e != nil {
			return e
		}
	}
	previous := s
	changed := !s.AutoRenew || s.PublishCert != cert || s.PublishKey != key || s.PublishCA != ""
	if changed {
		s.AutoRenew = true
		s.PublishCert = cert
		s.PublishKey = key
		s.PublishCA = ""
		if e = op.manager.Apply(s); e != nil {
			return e
		}
	}
	if e = host.VerifyServedCert(name, cert); e != nil {
		if changed {
			return errors.Join(e, op.manager.Apply(previous))
		}
		return e
	}
	return printJSON(map[string]any{"host": name, "serving_live_certificate": true, "action": op.path})
}
