package host

import (
	"bytes"
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
		m.report("Verifying staged sing-box")
		if e := verifyFile(p, s.CoreDigest); e != nil {
			return "", e
		}
		return p, nil
	}
	if m.Live {
		return m.artifact("sing-box")
	}
	return m.Core, nil
}
func (m Manager) stageBinary(prefix, file, want string, dry bool) error {
	if !digest.MatchString(want) {
		return fmt.Errorf("provide a trusted lowercase SHA-256 digest")
	}
	if dry {
		return verifyFile(file, want)
	}
	if m.Live {
		if err := prepareBin(); err != nil {
			return err
		}
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeVerified(filepath.Join(m.artifactDir(), prefix+want), f, want)
}

func (m Manager) StageCore(s *model.State, file, want string, dry bool) error {
	if err := m.stageBinary("core-", file, want, dry); err != nil || dry {
		return err
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
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		var buf bytes.Buffer
		e = copyVerified(&buf, f, want)
		candidate = buf.Bytes()
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
		if err := m.Service("status"); err != nil {
			e = fmt.Errorf("post-replacement systemd check failed: %w", err)
		}
	}
	if e != nil {
		return errors.Join(e, store.Write(path, old, 0755))
	}
	return nil
}
func (m Manager) StageConnector(s *model.State, file, want string, dry bool) error {
	if err := m.stageBinary("connector-", file, want, dry); err != nil || dry {
		return err
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
		m.report("Verifying staged cloudflared")
		return p, verifyFile(p, s.CloudDigest)
	}
	if m.Live {
		return m.artifact("cloudflared")
	}
	return "cloudflared", nil
}

func installWorker() (string, error) {
	p, e := os.Executable()
	if e != nil {
		return "", e
	}
	f, e := os.Open(p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	hash, e := copyArtifact(io.Discard, f)
	if e != nil {
		return "", e
	}
	target := filepath.Join(Bin, "worker-"+hash)
	if _, e = os.Stat(target); e == nil {
		return target, verifyFile(target, hash)
	} else if !os.IsNotExist(e) {
		return "", e
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return "", e
	}
	return target, writeVerified(target, f, hash)
}
