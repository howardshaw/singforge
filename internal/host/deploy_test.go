package host

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/store"
)

func TestUpdateServiceProbeUsesQuietStatus(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "current")
	if err := os.Mkdir(current, 0700); err != nil {
		t.Fatal(err)
	}
	for _, unit := range []string{coreUnit, subUnit} {
		if err := os.WriteFile(filepath.Join(current, unit), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\n[ \"$*\" = 'is-active --quiet " + coreUnit + " " + subUnit + "' ] && exit \"$PROBE_STATUS\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	m := Manager{Live: true, Dir: dir}
	t.Setenv("PROBE_STATUS", "0")
	if err := m.Service("status"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROBE_STATUS", "3")
	if err := m.Service("status"); err == nil {
		t.Fatal("lost systemctl failure")
	}
}

func TestRejectedGenerationAndRecovery(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "checker")
	if e := os.WriteFile(core, []byte("#!/bin/sh\nif [ \"$1\" = version ]; then echo 'sing-box version "+CoreVersion+"'; exit 0; fi\nexit 0\n"), 0700); e != nil {
		t.Fatal(e)
	}
	m := Manager{Dir: dir, Core: core}
	s, _ := model.New("::1")
	s.Enable("ss")
	if e := m.Apply(*s); e != nil {
		t.Fatal(e)
	}
	before, _ := os.Readlink(filepath.Join(dir, "current"))
	if e := os.WriteFile(core, []byte("#!/bin/sh\nif [ \"$1\" = version ]; then echo 'sing-box version "+CoreVersion+"'; else exit 1; fi\n"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := m.Apply(*s); e == nil {
		t.Fatal("accepted invalid candidate")
	}
	after, _ := os.Readlink(filepath.Join(dir, "current"))
	if after != before {
		t.Fatal("lost active generation")
	}
	store.Write(filepath.Join(dir, "transaction"), []byte(before), 0600)
	os.Remove(filepath.Join(dir, "current"))
	if e := m.Recover(); e != nil {
		t.Fatal(e)
	}
	if _, e := store.Read(dir); e != nil {
		t.Fatal(e)
	}
}
func TestTuningMemory(t *testing.T) {
	read := func(k string) (string, error) {
		switch k {
		case "net.ipv4.tcp_congestion_control":
			return "cubic", nil
		case "net.core.default_qdisc":
			return "fq", nil
		default:
			return "212992", nil
		}
	}
	k, e := knobs("streaming", 128*1024, read)
	if e != nil || len(k) != 3 || k[1].After != "524288" {
		t.Fatal(k, e)
	}
}

func TestDashboardConnectorUsesCredentialFile(t *testing.T) {
	s, _ := model.New("192.0.2.1")
	s.Tunnel = "edge.example.test"
	s.TunnelID = "7e127f9a-8a77-4332-bacd-86a2636f76ea"
	env, _ := json.Marshal(map[string]any{"a": strings.Repeat("a", 32), "t": s.TunnelID, "s": model.Random(32)})
	s.ConnectorToken = base64.StdEncoding.EncodeToString(env)
	for _, id := range []string{"vless-ws", "trojan-ws"} {
		s.Enable(id)
		s.Add(id, "tunnel", s.Tunnel, 443)
	}
	files, e := Files(*s, "sing-box", "cloudflared")
	if e != nil {
		t.Fatal(e)
	}
	unit := string(files[tunnelUnit])
	if strings.Contains(unit, s.ConnectorToken) || !strings.Contains(unit, "--token-file %d/connector.token") || string(files["connector.token"]) != s.ConnectorToken {
		t.Fatal("unsafe token delivery")
	}
	if _, e = ConnectorID("malformed"); e == nil {
		t.Fatal("accepted malformed token")
	}
}
