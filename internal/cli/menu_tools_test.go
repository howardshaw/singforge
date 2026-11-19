package cli

import (
	"bytes"
	tea "charm.land/bubbletea/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubscriptionMenuAndQR(t *testing.T) {
	m := dashboard{page: "home", width: 90, height: 40}
	m.acceptTool(choice{action: "subscriptions"})
	m.acceptTool(choice{action: "formats"})
	for _, item := range m.choices() {
		if !strings.HasPrefix(item.action, "issue ") {
			t.Fatal(item)
		}
	}
	m.acceptTool(m.choices()[0])
	if m.page != "tool-confirm" || strings.Join(m.pending, " ") != "subscription issue --format mihomo" {
		t.Fatal("unconfirmed issue command")
	}
	secret := "https://sub.example.org/s/example/mihomo"
	if !m.toolResult(menuResult{body: secret}) || m.page != "detail" || m.qr == "" {
		t.Fatal("missing URL/QR")
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = next.(dashboard)
	if m.page != "qr" {
		t.Fatal("QR shortcut")
	}
	p := filepath.Join(t.TempDir(), "private.png")
	if e := m.saveQR(p); e != nil {
		t.Fatal(e)
	}
	i, _ := os.Stat(p)
	b, _ := os.ReadFile(p)
	if i.Mode().Perm() != 0600 || !bytes.HasPrefix(b, []byte("\x89PNG")) {
		t.Fatal("private PNG")
	}
	if m.saveQR(p) == nil {
		t.Fatal("overwrote PNG")
	}
	m = m.back()
	if m.page != "formats" || m.payload != "" || m.qr != "" {
		t.Fatal("secret retained after back")
	}
}
func TestOfflineNodesAndUpdateConfirmation(t *testing.T) {
	m := dashboard{page: "subscriptions"}
	m.acceptTool(choice{action: "subscription nodes"})
	if !m.toolResult(menuResult{body: `["direct-ss"]`}) || m.choices()[0].action != "direct-ss" {
		t.Fatal("node picker")
	}
	m.page = "updates"
	for _, c := range m.choices() {
		v := m
		cmd, ok := v.acceptTool(c)
		if !ok || cmd != nil || v.page != "tool-confirm" || v.pending[len(v.pending)-1] != "--yes" {
			t.Fatal(c)
		}
	}
}
func TestRenewalFormUsesINIPathOnly(t *testing.T) {
	m := dashboard{page: "subscriptions"}
	m.acceptTool(choice{en: "Automatic certificate renewal", action: "renewal-enable"})
	if m.page != "renewal-credentials" {
		t.Fatal(m.page)
	}
	cmd, handled := m.inputTool("enter", "")
	if cmd != nil || !handled || m.page != "tool-confirm" || strings.Join(m.pending, " ") != "certificate auto-renew enable --credentials /etc/letsencrypt/singforge-dns.ini --yes" {
		t.Fatal(m.pending)
	}
}

func TestPublicationFormShowsCertbotPathsAndAcceptsCustomFiles(t *testing.T) {
	m := dashboard{page: "subscriptions", width: 100, height: 24}
	m.acceptTool(choice{action: "publish"})
	m.entry = "sub.example.test"
	m.inputTool("enter", "")
	dir := t.TempDir()
	for _, file := range []string{"fullchain.pem", "privkey.pem"} {
		view := m.View().Content
		if !strings.Contains(view, "/etc/letsencrypt/live/sub.example.test/"+file) || !strings.Contains(view, "sudo certbot certificates") {
			t.Fatal(view)
		}
		p := filepath.Join(dir, file)
		m.entry = p
		index := m.fieldIndex
		m.inputTool("enter", "")
		if m.problem == "" || m.fieldIndex != index {
			t.Fatal("accepted missing file")
		}
		if err := os.WriteFile(p, nil, 0600); err != nil {
			t.Fatal(err)
		}
		m.inputTool("enter", "")
	}
	if m.page != "tool-confirm" || strings.Join(m.pending, " ") != "subscription publish --host sub.example.test --cert "+filepath.Join(dir, "fullchain.pem")+" --key "+filepath.Join(dir, "privkey.pem") {
		t.Fatal(m.pending)
	}
}
