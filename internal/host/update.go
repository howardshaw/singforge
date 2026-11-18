package host

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/store"
)

var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var compatible = regexp.MustCompile(`(?m)^sing-box version 1\.14\.[0-9]+$`)

func verified(path, want string) ([]byte, error) {
	if !digest.MatchString(want) {
		return nil, fmt.Errorf("provide a trusted lowercase SHA-256 digest")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 201<<20))
	if e != nil {
		return nil, e
	}
	h := sha256.Sum256(b)
	if len(b) > 200<<20 || hex.EncodeToString(h[:]) != want {
		return nil, fmt.Errorf("artifact size or SHA-256 mismatch")
	}
	return b, nil
}
func (m Manager) artifactDir() string {
	if m.Live {
		return Bin
	}
	return m.Dir
}
func (m Manager) CorePath(s model.State) (string, error) {
	if s.CoreDigest != "" {
		if !digest.MatchString(s.CoreDigest) {
			return "", fmt.Errorf("invalid core digest")
		}
		p := filepath.Join(m.artifactDir(), "core-"+s.CoreDigest)
		if _, e := verified(p, s.CoreDigest); e != nil {
			return "", e
		}
		return p, nil
	}
	if m.Live {
		return Artifact("sing-box")
	}
	return m.Core, nil
}
func (m Manager) StageCore(s *model.State, file, want string, dry bool) error {
	b, e := verified(file, want)
	if e != nil {
		return e
	}
	if dry {
		return nil
	}
	if m.Live {
		if e = prepareBin(); e != nil {
			return e
		}
	}
	if e = store.Write(filepath.Join(m.artifactDir(), "core-"+want), b, 0755); e != nil {
		return e
	}
	s.PreviousCoreDigest = s.CoreDigest
	s.CoreDigest = want
	return nil
}
func (m Manager) SelfUpdate(file, want string, rollback, dry bool) error {
	if !m.Live {
		return fmt.Errorf("CLI replacement requires a system installation")
	}
	if e := prepareBin(); e != nil {
		return e
	}
	path, e := os.Executable()
	if e != nil {
		return e
	}
	if path != "/usr/local/bin/singforge" {
		return fmt.Errorf("self update requires /usr/local/bin/singforge")
	}
	previous := filepath.Join(Bin, "singforge.previous")
	var candidate []byte
	if rollback {
		candidate, e = os.ReadFile(previous)
	} else {
		candidate, e = verified(file, want)
	}
	if e != nil || dry {
		return e
	}
	stage, e := os.CreateTemp(Bin, ".cli-")
	if e != nil {
		return e
	}
	stage.Close()
	defer os.Remove(stage.Name())
	if e = store.Write(stage.Name(), candidate, 0755); e != nil {
		return e
	}
	probe := func(p string) error {
		out, e := exec.Command(p, "--version").CombinedOutput()
		if e != nil || !strings.Contains(string(out), "singforge") {
			return fmt.Errorf("CLI version probe failed")
		}
		return nil
	}
	if e = probe(stage.Name()); e != nil {
		return e
	}
	old, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	if e = store.Write(previous, old, 0755); e != nil {
		return e
	}
	if e = store.Write(path, candidate, 0755); e != nil {
		return e
	}
	if e = probe(path); e == nil {
		e = m.Service("status")
	}
	if e != nil {
		return errors.Join(e, store.Write(path, old, 0755))
	}
	return nil
}
func (m Manager) StageConnector(s *model.State, file, want string, dry bool) error {
	b, e := verified(file, want)
	if e != nil {
		return e
	}
	if dry {
		return nil
	}
	if m.Live {
		if e = prepareBin(); e != nil {
			return e
		}
	}
	if e = store.Write(filepath.Join(m.artifactDir(), "connector-"+want), b, 0755); e != nil {
		return e
	}
	s.PreviousCloudDigest = s.CloudDigest
	s.CloudDigest = want
	return nil
}
func (m Manager) connector(s model.State) (string, error) {
	if s.CloudDigest != "" {
		if !digest.MatchString(s.CloudDigest) {
			return "", fmt.Errorf("invalid connector digest")
		}
		p := filepath.Join(m.artifactDir(), "connector-"+s.CloudDigest)
		_, e := verified(p, s.CloudDigest)
		return p, e
	}
	if m.Live {
		return Artifact("cloudflared")
	}
	return "cloudflared", nil
}

func installWorker() (string, error) {
	p, e := os.Executable()
	if e != nil {
		return "", e
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return "", e
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(b))
	target := filepath.Join(Bin, "worker-"+hash)
	if _, e = os.Stat(target); e == nil {
		_, e = verified(target, hash)
		return target, e
	}
	return target, store.Write(target, b, 0755)
}
