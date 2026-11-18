package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/howardshaw/singforge/internal/model"
)

func TestLockBackupAndFileMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sf")
	u, e := Lock(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer u()
	if _, e = Lock(dir); e == nil {
		t.Fatal("two writers")
	}
	s, _ := model.New("::1")
	p := filepath.Join(dir, "backup")
	if e = Backup(*s, p); e != nil {
		t.Fatal(e)
	}
	if e = Backup(*s, p); e == nil {
		t.Fatal("overwrote backup")
	}
	i, _ := os.Stat(p)
	if i.Mode().Perm() != 0600 {
		t.Fatal(i.Mode())
	}
	v, e := Restore(p)
	if e != nil || v.Key != s.Key {
		t.Fatal("restore")
	}
	if e = Write(p, []byte(`{"SHA256":"wrong","State":{}}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Restore(p); e == nil {
		t.Fatal("accepted corrupted backup")
	}
}
