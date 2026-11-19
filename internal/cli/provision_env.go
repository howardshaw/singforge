package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/howardshaw/singforge/internal/model"
)

type provisionConfig struct {
	NodeDNS                                        string
	IP, Domain, Prefix, TokenFile, CertFile, Email string
	Tunnel, Publish, Agree                         bool
	Reality, HY2, SS                               int
}

func readProvision(path string) (provisionConfig, error) {
	var c provisionConfig
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return c, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return c, err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) || st.Size() > 65536 {
		return c, errors.New("env file must be a private regular file owned by the invoking user (chmod 600), at most 64 KiB")
	}
	return parseProvision(f)
}
func parseProvision(r io.Reader) (provisionConfig, error) {
	c := provisionConfig{Reality: 8881, HY2: 8882, SS: 8892}
	text := map[string]*string{"MIHOMO_NODE_DNS": &c.NodeDNS, "PUBLIC_IP": &c.IP, "DOMAIN": &c.Domain, "PREFIX": &c.Prefix, "CF_API_TOKEN_FILE": &c.TokenFile, "CERTBOT_CREDENTIALS_FILE": &c.CertFile, "ACME_EMAIL": &c.Email}
	toggles := map[string]*bool{"ENABLE_TUNNEL": &c.Tunnel, "ENABLE_SUBSCRIPTION": &c.Publish, "ACME_AGREE_TOS": &c.Agree}
	ports := map[string]*int{"REALITY_PORT": &c.Reality, "HYSTERIA2_PORT": &c.HY2, "SS_PORT": &c.SS}
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for line := 1; sc.Scan(); line++ {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		k, v, ok := strings.Cut(raw, "=")
		v = strings.TrimSpace(v)
		if !ok || seen[k] {
			return c, fmt.Errorf("invalid or duplicate setting at line %d", line)
		}
		seen[k] = true
		switch {
		case text[k] != nil:
			*text[k] = v
		case toggles[k] != nil:
			if v != "true" && v != "false" {
				return c, fmt.Errorf("%s requires true or false", k)
			}
			*toggles[k] = v == "true"
		case ports[k] != nil:
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 65535 {
				return c, fmt.Errorf("invalid %s", k)
			}
			*ports[k] = n
		default:
			return c, fmt.Errorf("unknown setting at line %d", line)
		}
	}
	if err := sc.Err(); err != nil {
		return c, err
	}
	if c.NodeDNS != "" && c.NodeDNS != "default" && c.NodeDNS != "alidns" {
		return c, errors.New("invalid MIHOMO_NODE_DNS")
	}
	addr, err := netip.ParseAddr(c.IP)
	if err != nil || addr.Zone() != "" || addr.IsUnspecified() || addr.IsMulticast() {
		return c, errors.New("set PUBLIC_IP to the VPS address")
	}
	if c.Reality == c.SS || c.HY2 == c.SS || c.Publish && (c.Reality == 443 || c.SS == 443) {
		return c, errors.New("local service ports overlap")
	}
	if c.Domain != "" {
		if !model.Domain(c.Domain) || !model.Domain(c.hostname("fast")) || c.Prefix == "" || strings.Contains(c.Prefix, ".") || !filepath.IsAbs(c.TokenFile) {
			return c, errors.New("set DOMAIN, a DNS-label PREFIX and absolute CF_API_TOKEN_FILE")
		}
	} else if c.Tunnel || c.Publish {
		return c, errors.New("set DOMAIN to enable Cloudflare features")
	}
	if c.Publish {
		email, err := mail.ParseAddress(c.Email)
		if err != nil || email.Address != c.Email || !c.Agree || !filepath.IsAbs(c.CertFile) {
			return c, errors.New("publication requires ACME_EMAIL, ACME_AGREE_TOS=true and absolute CERTBOT_CREDENTIALS_FILE")
		}
	}
	return c, nil
}
func (c provisionConfig) hostname(role string) string { return c.Prefix + "-" + role + "." + c.Domain }
