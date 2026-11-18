package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCobraSurface(t *testing.T) {
	for _, spec := range commands {
		c, rest, e := NewCommand().Find(strings.Fields(spec.path))
		if e != nil || len(rest) != 0 || c.CommandPath() != "singforge "+spec.path {
			t.Fatal(spec.path, e)
		}
	}
	for _, args := range [][]string{{"protocol", "nonsense"}, {"status", "--qr"}, {"subscription", "publish", "--host", "example.org"}} {
		if e := Run(args); e == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	c := NewCommand()
	var b bytes.Buffer
	c.SetOut(&b)
	c.SetArgs([]string{"subscription", "show", "--help"})
	if e := c.Execute(); e != nil || !strings.Contains(b.String(), "--qr") || strings.Contains(b.String(), "--credentials") {
		t.Fatal("command-specific help")
	}
}
func TestCobraPreview(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ws")
	args := []string{"--workspace", dir, "install", "--dry-run", "--host", "2001:db8::1", "--preset", "custom", "--protocols", "ss,tuic"}
	if e := Run(args); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(dir, "current")); !os.IsNotExist(e) {
		t.Fatal("dry-run committed")
	}
}
