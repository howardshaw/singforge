package host

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/store"
)

const installedCLI = "/usr/local/bin/singforge"

var binaryName = regexp.MustCompile(`^((core|worker|connector)-[0-9a-f]{64}|(sing-box|cloudflared)-[0-9a-f]{16}|singforge\.previous)$`)
var generatedDir = regexp.MustCompile(`^gen-[0-9]+$`)
var stageDir = regexp.MustCompile(`^\.(nginx|update)-[0-9]+$`)
var temporaryFile = regexp.MustCompile(`^\.(pending|download|cli|package)-[0-9]+$`)

// LocalRemoval describes an offline uninstall. Paths are collected without
// following directory symlinks. Unknown files and operator backups stay in place.
type LocalRemoval struct {
	Units       []string `json:"units,omitempty"`
	Files       []string `json:"files,omitempty"`
	Directories []string `json:"remove_if_empty,omitempty"`
	Restore     []string `json:"restore,omitempty"`
	Preserved   []string `json:"preserved,omitempty"`
	CLI         string   `json:"installed_cli,omitempty"`
}

func entries(dir string) ([]os.DirEntry, error) {
	st, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("expected a directory: %s", dir)
	}
	return os.ReadDir(dir)
}

func (p *LocalRemoval) collect(dir string, known func(string) bool) error {
	files, err := entries(dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		path := filepath.Join(dir, f.Name())
		if f.Type().IsRegular() && known(f.Name()) {
			p.Files = append(p.Files, path)
		} else {
			p.Preserved = append(p.Preserved, path)
		}
	}
	p.Directories = append(p.Directories, dir)
	return nil
}

const workspaceMarker = ".singforge-workspace"
const workspaceSignature = "SingForge workspace v1\n"

func markWorkspace(dir string) error {
	path := filepath.Join(dir, workspaceMarker)
	if _, err := os.Lstat(path); err == nil {
		if !validMarker(path, workspaceSignature) {
			return fmt.Errorf("unmanaged marker %s", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return store.Write(path, []byte(workspaceSignature), 0600)
}
func validMarker(path, signature string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && bytes.Equal(data, []byte(signature))
}

func checkWorkspaceRemoval(dir string) error {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	for _, system := range []string{store.Root, Bin} {
		if resolved == system || strings.HasPrefix(resolved, system+string(os.PathSeparator)) {
			return fmt.Errorf("system installation requires live mode")
		}
	}
	if validMarker(filepath.Join(dir, workspaceMarker), workspaceSignature) {
		return nil
	}
	// Older installations predate the workspace marker. Verify their current
	// generation rather than treating any private directory as a deployment.
	gen, err := os.Readlink(filepath.Join(dir, "current"))
	if err == nil && generatedDir.MatchString(gen) {
		st, err := os.Lstat(filepath.Join(dir, gen))
		if err == nil && st.IsDir() {
			if _, err = store.Read(dir); err == nil {
				return nil
			}
		}
	}
	return fmt.Errorf("no recognized SingForge workspace at %s", dir)
}

func (p *LocalRemoval) state(dir string, live bool) error {
	files, err := entries(dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		n := f.Name()
		path := filepath.Join(dir, n)
		switch {
		case !live && n == workspaceMarker && validMarker(path, workspaceSignature): // Removed after other workspace files.
		case n == "lock": // Unlink last while still holding its flock.
		case n == "current" || n == ".next":
			dest, err := os.Readlink(path)
			if err != nil || !generatedDir.MatchString(dest) {
				return fmt.Errorf("unmanaged generation link: %s", path)
			}
			p.Files = append(p.Files, path)
		case f.IsDir() && generatedDir.MatchString(n):
			if err := p.collect(path, func(name string) bool { return generationFile(name) || temporaryFile.MatchString(name) }); err != nil {
				return err
			}
		case f.IsDir() && stageDir.MatchString(n):
			if err := p.collect(path, func(name string) bool {
				return slices.Contains([]string{"origin.crt", "origin.key", "subscription.crt", "subscription.key", "nginx-check.conf", "sing-box", "singforge", "cloudflared"}, name) || temporaryFile.MatchString(name)
			}); err != nil {
				return err
			}
		case live && (n == "tuning.json" || n == "firewall.json"):
			if !f.Type().IsRegular() {
				return fmt.Errorf("invalid restoration ledger %s", path)
			}
			p.Restore = append(p.Restore, n)
		case f.Type().IsRegular() && (slices.Contains([]string{"cloudflare.token", "created-tunnel.json", "dns-owned.json", "transaction"}, n) || temporaryFile.MatchString(n) || (!live && (strings.HasPrefix(n, "core-") || strings.HasPrefix(n, "connector-")) && binaryName.MatchString(n))):
			p.Files = append(p.Files, path)
		default:
			p.Preserved = append(p.Preserved, path)
		}
	}
	return nil
}

func (p *LocalRemoval) binaries(dir string) error {
	files, err := entries(dir)
	if err != nil {
		return err
	}
	if files == nil {
		return nil
	}
	marker := filepath.Join(dir, ".singforge-owned")
	info, err := os.Lstat(marker)
	if os.IsNotExist(err) {
		p.Preserved = append(p.Preserved, dir)
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("invalid ownership marker: %s", marker)
	}
	b, err := os.ReadFile(marker)
	if err != nil || string(b) != "1\n" {
		return fmt.Errorf("invalid ownership marker: %s", marker)
	}
	for _, f := range files {
		path := filepath.Join(dir, f.Name())
		if path == marker {
			continue
		}
		if f.Type().IsRegular() && (binaryName.MatchString(f.Name()) || temporaryFile.MatchString(f.Name())) {
			p.Files = append(p.Files, path)
		} else {
			p.Preserved = append(p.Preserved, path)
		}
	}
	p.Files = append(p.Files, marker)
	p.Directories = append(p.Directories, dir)
	return nil
}

func (m Manager) RemovalPlan() (LocalRemoval, error) {
	var p LocalRemoval
	if !m.Live {
		if err := checkWorkspaceRemoval(m.Dir); err != nil {
			return p, err
		}
	}
	if err := p.state(m.Dir, m.Live); err != nil {
		return p, err
	}
	if m.Live {
		if m.Dir != store.Root {
			return p, fmt.Errorf("invalid live state directory")
		}
		for _, unit := range []string{rulesTimer, rulesService, tunnelUnit, edgeUnit, subUnit, coreUnit} {
			link := unitLink(unit)
			dest, err := os.Readlink(link)
			switch {
			case err == nil && dest == filepath.Join(m.Dir, "current", unit):
				p.Units = append(p.Units, unit)
			case os.IsNotExist(err):
				// A prior removal may have already removed the main unit link.
			case err == nil || errors.Is(err, syscall.EINVAL):
				p.Preserved = append(p.Preserved, link)
				continue
			default:
				return p, err
			}
			boot := bootPath(unit)
			target, e := os.Readlink(boot)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil || !m.ownsBootTarget(unit, target) {
				return p, fmt.Errorf("unmanaged boot link %s", boot)
			}
			if os.IsNotExist(err) {
				p.Files = append(p.Files, boot)
			}
		}
		if RenewalHookInstalled(RenewalHook) {
			p.Files = append(p.Files, RenewalHook)
		}
		if err := p.binaries(Bin); err != nil {
			return p, err
		}
		info, err := os.Lstat(installedCLI)
		if err == nil {
			running, e := os.Stat("/proc/self/exe")
			if e != nil || !os.SameFile(info, running) {
				return p, fmt.Errorf("run the installed %s to remove it", installedCLI)
			}
			p.CLI = installedCLI
		} else if !os.IsNotExist(err) {
			return p, err
		}
	}
	if !m.Live && validMarker(filepath.Join(m.Dir, workspaceMarker), workspaceSignature) {
		p.Files = append(p.Files, filepath.Join(m.Dir, workspaceMarker))
	}
	p.Directories = append(p.Directories, m.Dir)
	return p, nil
}

// RemoveLocal runs with the deployment lock held. The full-uninstall entry
// point also works after --purge or an interrupted removal with no current link.
func (m Manager) RemoveLocal() (LocalRemoval, error) {
	p, err := m.RemovalPlan()
	if err != nil {
		return p, err
	}
	if !m.Live && !validMarker(filepath.Join(m.Dir, workspaceMarker), workspaceSignature) {
		if err = markWorkspace(m.Dir); err != nil {
			return p, err
		}
		p.Files = append(p.Files, filepath.Join(m.Dir, workspaceMarker))
	}
	if m.Live {
		for _, unit := range p.Units {
			if err = m.detach(unit); err != nil {
				return p, err
			}
		}
		if err = run("systemctl", "daemon-reload"); err != nil {
			return p, err
		}
		if _, err = Firewall(m.Dir, model.State{}, true, false, true); err != nil {
			return p, err
		}
		if _, err = Tune(m.Dir, "disable", "", false, true); err != nil {
			return p, err
		}
	}
	for _, path := range p.Files {
		if err = os.Remove(path); err != nil {
			return p, err
		}
	}
	for _, dir := range p.Directories {
		if dir == m.Dir {
			continue
		}
		if err = removeEmpty(dir); err != nil {
			return p, err
		}
	}
	if p.CLI != "" {
		if err = os.Remove(p.CLI); err != nil {
			return p, err
		}
	}
	if err = os.Remove(filepath.Join(m.Dir, "lock")); err != nil {
		return p, err
	}
	return p, removeEmpty(m.Dir)
}
func removeEmpty(dir string) error {
	err := os.Remove(dir)
	if errors.Is(err, syscall.ENOTEMPTY) || os.IsNotExist(err) {
		return nil
	}
	return err
}
