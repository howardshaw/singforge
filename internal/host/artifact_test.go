package host

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/howardshaw/singforge/internal/model"
)

// Exercise the same cache-hit path used by every provision milestone.
func BenchmarkCachedCore(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "candidate")
	f, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	const size = 81416421 // pinned Linux amd64 sing-box executable
	if err := f.Truncate(size); err != nil {
		b.Fatal(err)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		b.Fatal(err)
	}
	want := fmt.Sprintf("%x", h.Sum(nil))
	if err := os.Rename(path, filepath.Join(dir, "core-"+want)); err != nil {
		b.Fatal(err)
	}
	m := Manager{Dir: dir}
	s := model.State{CoreDigest: want}
	b.SetBytes(size)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.CorePath(s); err != nil {
			b.Fatal(err)
		}
	}
}

func TestWriteVerifiedPreservesInstalledBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binary")
	const good = "verified executable"
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(good)))
	if err := writeVerified(path, strings.NewReader(good), want); err != nil {
		t.Fatal(err)
	}
	if err := writeVerified(path, strings.NewReader("damaged"), want); err == nil {
		t.Fatal("accepted a digest mismatch")
	}
	if err := verifyFile(path, want); err != nil {
		t.Fatalf("lost the verified executable: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("unexpected staging files: %v, %v", entries, err)
	}
}

func TestOversizedArtifact(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "oversized")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(maxArtifactSize + 1); err != nil {
		t.Fatal(err)
	}
	if _, err := copyArtifact(io.Discard, f); err == nil {
		t.Fatal("accepted an oversized artifact")
	}
}
