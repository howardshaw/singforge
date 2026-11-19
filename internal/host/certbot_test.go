package host

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http/httptest"

	"github.com/howardshaw/singforge/internal/model"
	"os"
	"path/filepath"
	"testing"
)

func TestHookOwnership(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hook")
	if err := InstallRenewalHook(p); err != nil {
		t.Fatal(err)
	}
	if !RenewalHookInstalled(p) {
		t.Fatal("missing executable")
	}
	if err := InstallRenewalHook(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := InstallRenewalHook(p); err == nil {
		t.Fatal("overwrote operator hook")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "#!/bin/sh\nexit 0\n" {
		t.Fatal("changed foreign hook")
	}
}
func TestRenewalParams(t *testing.T) {
	m := renewalParams("authenticator = ignored\n[renewalparams]\nauthenticator = dns-cloudflare\ndns_cloudflare_credentials = /etc/letsencrypt/singforge-dns.ini\ndns_cloudflare_propagation_seconds = 60\nunrelated_secret = secret\n[[webroot_map]]\nauthenticator = ignored\n")
	if len(m) != 3 || m["authenticator"] != "dns-cloudflare" || m["dns_cloudflare_propagation_seconds"] != "60" {
		t.Fatal(m)
	}
}
func TestServedCertificateIdentity(t *testing.T) {
	srv := httptest.NewTLSServer(nil)
	defer srv.Close()
	a := srv.Certificate()
	roots := x509.NewCertPool()
	roots.AddCert(a)
	cfg := &tls.Config{RootCAs: roots, ServerName: "example.com", MinVersion: tls.VersionTLS12}
	cert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.Raw}))
	if e := verifyServedAt(srv.Listener.Addr().String(), cfg, cert); e != nil {
		t.Fatal(e)
	}
	other, e := model.New("127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	if verifyServedAt(srv.Listener.Addr().String(), cfg, other.Certificate) == nil {
		t.Fatal("different active certificate accepted")
	}
}
func TestCertbotRequiresScopedTokenFormat(t *testing.T) {
	if e := validateTokenINI("# Certbot credential\n dns_cloudflare_api_token = example-value\n"); e != nil {
		t.Fatal(e)
	}
	for _, b := range []string{"", "dns_cloudflare_api_key = global", "dns_cloudflare_api_token = ", "dns_cloudflare_api_token = one\ndns_cloudflare_api_token = two"} {
		if validateTokenINI(b) == nil {
			t.Fatal("accepted invalid credential format")
		}
	}
}
