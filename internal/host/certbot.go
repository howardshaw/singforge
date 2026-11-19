package host

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const RenewalHook = "/etc/letsencrypt/renewal-hooks/deploy/80-singforge-subscription"
const hookProgram = `#!/bin/sh
# Managed by SingForge. Certbot may also renew unrelated certificates.
set -eu
marker=/var/lib/singforge/current/renewal-lineage
[ -r "$marker" ] || exit 0
[ "${RENEWED_LINEAGE:-}" = "$(cat "$marker")" ] || exit 0
exec /usr/local/bin/singforge certificate auto-renew sync
`

func Lineage(host string) string { return filepath.Join("/etc/letsencrypt/live", host) }
func RenewalHookInstalled(path string) bool {
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0755 || st.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return false
	}
	data, e := os.ReadFile(path)
	return e == nil && string(data) == hookProgram
}

// Link an already complete executable into place without clobbering a preexisting hook.
func InstallRenewalHook(path string) error {
	if RenewalHookInstalled(path) {
		return nil
	}
	dir := filepath.Dir(path)
	f, e := os.CreateTemp(dir, ".singforge-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.WriteString(hookProgram)
	if e == nil {
		e = f.Chmod(0755)
	}
	if e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	if e != nil {
		return e
	}
	if e = os.Link(f.Name(), path); e != nil {
		return fmt.Errorf("cannot install owned Certbot deploy hook: %w", e)
	}
	return nil
}
func RemoveRenewalHook() error {
	if RenewalHookInstalled(RenewalHook) {
		return os.Remove(RenewalHook)
	}
	return nil
}
func ValidateCredentialFile(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("supply a clean absolute credentials path")
	}
	for p := path; ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if info.Sys().(*syscall.Stat_t).Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("credentials and parent directories must be controlled by root")
		}
		if p == path {
			if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 8192 {
				return fmt.Errorf("expected a root-private credentials file (0600 or 0400)")
			}
		} else if !info.IsDir() {
			return fmt.Errorf("credentials path contains a symlink or invalid directory")
		}
		if p == "/" {
			break
		}
	}
	return nil
}
func ValidateDNSCredentials(path string) error {
	if err := ValidateCredentialFile(path); err != nil {
		return err
	}
	data, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return validateTokenINI(string(data))
}
func validateTokenINI(data string) error {
	found := false
	for line := range strings.SplitSeq(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if found || !ok || strings.TrimSpace(k) != "dns_cloudflare_api_token" || strings.TrimSpace(v) == "" {
			return fmt.Errorf("expected only dns_cloudflare_api_token in credentials INI")
		}
		found = true
	}
	if !found {
		return fmt.Errorf("missing DNS API token")
	}
	return nil
}
func RenewalParams(host string) (map[string]string, error) {
	data, e := os.ReadFile(filepath.Join("/etc/letsencrypt/renewal", host+".conf"))
	if e != nil {
		return nil, e
	}
	return renewalParams(string(data)), nil
}
func renewalParams(data string) map[string]string {
	out := map[string]string{}
	section := ""
	for line := range strings.SplitSeq(data, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if section != "[renewalparams]" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if ok && (k == "authenticator" || k == "dns_cloudflare_credentials" || k == "dns_cloudflare_propagation_seconds") {
			out[k] = strings.Trim(strings.TrimSpace(v), "\"'")
		}
	}
	return out
}
func VerifyServedCert(host, expected string) error {
	return verifyServedAt("127.0.0.1:443", &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}, expected)
}
func verifyServedAt(addr string, config *tls.Config, expected string) error {
	block, _ := pem.Decode([]byte(expected))
	if block == nil {
		return fmt.Errorf("invalid expected certificate")
	}
	leaf, e := x509.ParseCertificate(block.Bytes)
	if e != nil {
		return e
	}
	client, e := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr, config)
	if e != nil {
		return fmt.Errorf("local HTTPS handshake failed: %w", e)
	}
	defer client.Close()
	if !bytes.Equal(client.ConnectionState().PeerCertificates[0].Raw, leaf.Raw) {
		return fmt.Errorf("HTTPS origin is serving a different certificate")
	}
	return nil
}
