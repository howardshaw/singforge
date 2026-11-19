package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/howardshaw/singforge/internal/cloudflare"
	"github.com/howardshaw/singforge/internal/model"
)

func TestProvisionCloudPreflightReadsAllEnabledResources(t *testing.T) {
	const tunnelID = "4ac0ac32-49c8-42e1-8e45-c1e4cb23b97f"
	id := strings.Repeat("a", 32)
	c := provisionConfig{Domain: "example.org", Prefix: "node", IP: "192.0.2.1", Tunnel: true, Publish: true}
	dir := t.TempDir()
	var reads []string
	unownedIP := true
	remote := cloudflare.Tunnel{Name: c.hostname("edge"), ID: tunnelID, ConfigSource: "local", Status: "healthy"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("preflight issued %s", r.Method)
		}
		var result any
		switch {
		case r.URL.Path == "/zones":
			result = []any{map[string]any{"id": id, "name": c.Domain, "account": map[string]string{"id": id}}}
		case strings.HasSuffix(r.URL.Path, "/cfd_tunnel"):
			result = []cloudflare.Tunnel{remote}
		case strings.HasSuffix(r.URL.Path, "/dns_records"):
			name := r.URL.Query().Get("name")
			reads = append(reads, name)
			result = []cloudflare.Record{}
			if name == c.hostname("edge") {
				result = []cloudflare.Record{{ID: id, Name: name, Type: "CNAME", Content: tunnelID + ".cfargotunnel.com", Proxied: true}}
			}
			if name == c.hostname("sub") && unownedIP {
				result = []cloudflare.Record{{ID: id, Name: name, Type: "A", Content: "192.0.2.2"}}
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer srv.Close()
	client := cloudflare.Client{Base: srv.URL, HTTP: srv.Client()}
	s := model.State{Direct: c.hostname("fast"), CDN: "cdn.example.org"}
	_, err := checkProvisionCloud(client, c, &s, dir)
	if err == nil || !strings.Contains(err.Error(), tunnelID) || !strings.Contains(err.Error(), c.hostname("sub")) || strings.Contains(err.Error(), "conflict with existing CNAME") {
		t.Fatal(err)
	}
	slices.Sort(reads)
	if strings.Join(reads, ",") != "cdn.example.org,node-edge.example.org,node-fast.example.org,node-sub.example.org" {
		t.Fatal(reads)
	}
	remote.Status = "down"
	if recovery, err := checkProvisionCloud(client, c, &s, dir); err == nil || recovery != "" {
		t.Fatal("DNS conflict must block recovery")
	}
	unownedIP = false
	if recovery, err := checkProvisionCloud(client, c, &s, dir); err != nil || recovery != tunnelID || s.TunnelID != "" || s.TunnelCredentials != "" {
		t.Fatal("expected read-only recovery plan", recovery, err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatal("preflight persisted credentials")
	}
	remote.ConfigSource = "cloudflare"
	if _, err := checkProvisionCloud(client, c, &s, dir); err == nil {
		t.Fatal("unexpected recovery for dashboard-managed tunnel")
	}
	raw := fmt.Sprintf(`{"AccountTag":%q,"TunnelID":%q,"TunnelSecret":%q}`, id, tunnelID, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err := os.WriteFile(filepath.Join(dir, "created-tunnel.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	remote.ConfigSource = "local"
	if _, err := checkProvisionCloud(client, c, &s, dir); err != nil || s.TunnelID != tunnelID {
		t.Fatal(err, s.TunnelID)
	}
}
