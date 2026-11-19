package host

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/howardshaw/singforge/internal/store"
)

func TestFullWorkspaceUninstall(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	unlock, err := store.Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err = markWorkspace(root); err != nil {
		t.Fatal(err)
	}
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"gen-123", "gen-456", ".nginx-789"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"dns-owned.json", "cloudflare.token", "created-tunnel.json", "core-" + strings.Repeat("a", 64), "gen-123/state.json", "gen-123/bundle.json", "gen-456/connector.token", ".nginx-789/origin.key", "keep.backup", "gen-123/notes"} {
		write(path)
	}
	if err = os.Symlink("gen-123", filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err = os.Symlink(outside, filepath.Join(root, "gen-987")); err != nil {
		t.Fatal(err)
	}
	m := Manager{Dir: root}
	p, err := m.RemovalPlan()
	if err != nil || !slices.Contains(p.Files, filepath.Join(root, "cloudflare.token")) {
		t.Fatal(err, p)
	}
	if _, err = os.Stat(filepath.Join(root, "cloudflare.token")); err != nil {
		t.Fatal("dry run removed token")
	}
	if _, err = m.RemoveLocal(); err != nil {
		t.Fatal(err)
	}
	for _, path := range p.Files {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("did not remove", path, err)
		}
	}
	for _, path := range []string{"keep.backup", "gen-123/notes", "gen-987"} {
		if _, err := os.Lstat(filepath.Join(root, path)); err != nil {
			t.Fatal(path, err)
		}
	}
}

func TestFullRemovalOwnership(t *testing.T) {
	dir := t.TempDir()
	if _, err := (Manager{Dir: dir}).RemovalPlan(); err == nil {
		t.Fatal("accepted unmanaged workspace")
	}
	var p LocalRemoval
	if err := p.binaries(dir); err != nil || len(p.Files) != 0 {
		t.Fatal("accepted unmanaged binary directory", err)
	}
	file := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	file(".singforge-owned", "1\n")
	file("worker-"+strings.Repeat("b", 64), "worker")
	file("user-backup", "keep")
	p = LocalRemoval{}
	if err := p.binaries(dir); err != nil || len(p.Files) != 2 || !slices.Contains(p.Preserved, filepath.Join(dir, "user-backup")) {
		t.Fatal(p, err)
	}
}
