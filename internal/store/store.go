package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/howardshaw/singforge/internal/model"
)

const Root = "/var/lib/singforge"

func PrivateDir(dir string) error {
	if e := os.Mkdir(dir, 0700); e != nil && !os.IsExist(e) {
		return e
	}
	i, e := os.Lstat(dir)
	if e != nil {
		return e
	}
	if !i.IsDir() || i.Mode().Perm()&0077 != 0 || i.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("expected private owned directory %s", dir)
	}
	return nil
}
func Lock(dir string) (func(), error) { return LockFor(dir, 0) }

// LockFor allows an unattended deploy hook to wait for an in-flight transaction.
func LockFor(dir string, wait time.Duration) (func(), error) {
	if e := PrivateDir(dir); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	deadline := time.Now().Add(wait)
	for {
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			break
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, e
		}
		if !time.Now().Before(deadline) {
			f.Close()
			return nil, fmt.Errorf("another operation holds the lock")
		}
		time.Sleep(min(100*time.Millisecond, time.Until(deadline)))
	}
	// A full uninstall can retire the lock while a deploy hook is waiting.
	held, err := f.Stat()
	current, lookup := os.Lstat(filepath.Join(dir, "lock"))
	if err != nil || lookup != nil || !os.SameFile(held, current) {
		f.Close()
		return nil, fmt.Errorf("deployment lock was replaced; retry the operation")
	}
	return func() { f.Close() }, nil
}
func Sync(dir string) error {
	d, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func Write(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, e := os.CreateTemp(dir, ".pending-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(data); e == nil {
		e = f.Chmod(mode)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = os.Rename(f.Name(), path)
	}
	if e == nil {
		e = Sync(dir)
	}
	return e
}
func Read(dir string) (model.State, error) {
	var s model.State
	b, e := os.ReadFile(filepath.Join(dir, "current", "state.json"))
	if e == nil {
		e = json.Unmarshal(b, &s)
	}
	if e == nil {
		e = s.Validate()
	}
	return s, e
}

type backup struct {
	SHA256 string
	State  json.RawMessage
}

func Backup(s model.State, path string) error {
	b, e := json.Marshal(s)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(b)
	out, e := json.Marshal(backup{hex.EncodeToString(sum[:]), b})
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(out)
	if e == nil {
		e = f.Sync()
	}
	return errors.Join(e, f.Close())
}
func Restore(path string) (model.State, error) {
	var s model.State
	var v backup
	b, e := os.ReadFile(path)
	if e != nil {
		return s, e
	}
	if e = json.Unmarshal(b, &v); e != nil {
		return s, e
	}
	sum := sha256.Sum256(v.State)
	if v.SHA256 != hex.EncodeToString(sum[:]) {
		return s, fmt.Errorf("backup digest mismatch")
	}
	if e = json.Unmarshal(v.State, &s); e != nil {
		return s, e
	}
	return s, s.Validate()
}
