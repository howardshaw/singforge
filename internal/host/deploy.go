package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/store"
)

type Manager struct {
	Dir, Core string
	Live      bool
}

func run(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if e := exec.CommandContext(ctx, name, args...).Run(); e != nil {
		return fmt.Errorf("%s failed (%w)", filepath.Base(name), e)
	}
	return nil
}
func Preflight() error {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return fmt.Errorf("host deployment requires Linux and root; run with sudo")
	}
	if _, e := os.Stat("/run/systemd/system"); e != nil {
		return fmt.Errorf("systemd must manage the host")
	}
	data, e := os.ReadFile("/etc/os-release")
	if e != nil {
		return e
	}
	var id, vers string
	for l := range strings.SplitSeq(string(data), "\n") {
		k, v, _ := strings.Cut(l, "=")
		v = strings.Trim(v, "\"")
		switch k {
		case "ID":
			id = v
		case "VERSION_ID":
			vers = v
		}
	}
	var major int
	fmt.Sscanf(vers, "%d", &major)
	if !(id == "debian" && major >= 12 || id == "ubuntu" && major >= 22) {
		return fmt.Errorf("requires Debian 12+ or Ubuntu 22.04+")
	}
	return nil
}
func prepareBin() error {
	if e := os.MkdirAll(filepath.Dir(Bin), 0755); e != nil {
		return e
	}
	e := os.Mkdir(Bin, 0755)
	mark := filepath.Join(Bin, ".singforge-owned")
	if e == nil {
		if e = store.Write(mark, []byte("1\n"), 0644); e != nil {
			return e
		}
	} else if !os.IsExist(e) {
		return e
	}
	st, e := os.Lstat(Bin)
	if e != nil {
		return e
	}
	if !st.IsDir() || !validMarker(mark, "1\n") {
		return fmt.Errorf("existing unmanaged binary directory %s", Bin)
	}
	// DynamicUser services must be able to traverse the binary directory
	// even when the installation was run with umask 077.
	return os.Chmod(Bin, 0755)
}
func (m Manager) binaries(s model.State) (core, cloud string, err error) {
	if !m.Live {
		core, err = m.CorePath(s)
		if err != nil {
			return
		}
		cloud, err = m.connector(s)
		return
	}
	if e := prepareBin(); e != nil {
		return "", "", e
	}
	core, err = m.CorePath(s)
	if err != nil {
		return
	}
	if s.Tunnel != "" {
		cloud, err = m.connector(s)
		if err != nil {
			return
		}
	}

	return
}
func unitLink(name string) string { return filepath.Join("/etc/systemd/system", name) }
func (m Manager) checkLinks() error {
	for _, n := range all {
		p := unitLink(n)
		v, e := os.Readlink(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil || v != filepath.Join(m.Dir, "current", n) {
			return fmt.Errorf("unmanaged systemd unit %s", p)
		}
	}
	return nil
}
func (m Manager) switchTo(gen string) error {
	link := filepath.Join(m.Dir, "current")
	if gen == "" {
		e := os.Remove(link)
		if os.IsNotExist(e) {
			return nil
		}
		return e
	}
	if filepath.Base(gen) != gen || !strings.HasPrefix(gen, "gen-") {
		return fmt.Errorf("invalid generation reference")
	}
	temp := filepath.Join(m.Dir, ".next")
	if e := os.Remove(temp); e != nil && !os.IsNotExist(e) {
		return e
	}
	if e := os.Symlink(gen, temp); e != nil {
		return e
	}
	if e := os.Rename(temp, link); e != nil {
		return e
	}
	return store.Sync(m.Dir)
}

// The wants link must survive generation garbage collection. systemctl enable
// resolves the unit symlink and can otherwise pin a soon-to-be-deleted generation.
func bootPath(n string) string {
	target := "multi-user.target.wants"
	if n == rulesTimer {
		target = "timers.target.wants"
	}
	return filepath.Join("/etc/systemd/system", target, n)
}
func (m Manager) ownsBootTarget(n, v string) bool {
	parent := filepath.Dir(v)
	return v == "../"+n || v == unitLink(n) || (filepath.Base(v) == n && filepath.Dir(parent) == m.Dir && generatedDir.MatchString(filepath.Base(parent)))
}
func (m Manager) bootLink(n string, enabled bool) error {
	p := bootPath(n)
	want := "../" + n
	v, e := os.Readlink(p)
	if e == nil {
		if v == want && enabled {
			return nil
		}
		if !m.ownsBootTarget(n, v) {
			return fmt.Errorf("unmanaged boot link %s", p)
		}
		if e = os.Remove(p); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if enabled {
		return os.Symlink(want, p)
	}
	return nil
}
func (m Manager) detach(n string) error {
	dst := unitLink(n)
	if _, e := os.Lstat(dst); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	load, err := exec.Command("systemctl", "show", "-p", "LoadState", "--value", n).Output()
	if err != nil && strings.TrimSpace(string(load)) != "not-found" {
		return err
	}
	if strings.TrimSpace(string(load)) != "not-found" {
		if e := run("systemctl", "stop", n); e != nil {
			return e
		}
	}
	if e := m.bootLink(n, false); e != nil {
		return e
	}
	return os.Remove(dst)
}
func (m Manager) changed(old, n string) bool {
	if old == "" {
		return true
	}
	inputs := map[string][]string{coreUnit: {"server.json"}, edgeUnit: {"nginx.conf", "origin.crt", "origin.key", "subscription.crt", "subscription.key"}, subUnit: {"bundle.json"}, tunnelUnit: {"tunnel.yml", "tunnel.json", "connector.token"}}
	for _, f := range append(inputs[n], n) {
		a, ea := os.ReadFile(filepath.Join(m.Dir, old, f))
		b, eb := os.ReadFile(filepath.Join(m.Dir, "current", f))
		if os.IsNotExist(ea) && os.IsNotExist(eb) {
			continue
		}
		if ea != nil || eb != nil || !bytes.Equal(a, b) {
			return true
		}
	}
	return false
}
func (m Manager) reconcile(old string) error {
	if !m.Live {
		return nil
	}
	if e := m.checkLinks(); e != nil {
		return e
	}
	var enabled []string
	for _, n := range all {
		src := filepath.Join(m.Dir, "current", n)
		_, e := os.Stat(src)
		if os.IsNotExist(e) {
			if e = m.detach(n); e != nil {
				return e
			}
			continue
		}
		if e != nil {
			return e
		}
		dst := unitLink(n)
		if _, e = os.Lstat(dst); os.IsNotExist(e) {
			if e = os.Symlink(src, dst); e != nil {
				return e
			}
		}
		if n == rulesService {
			continue
		}
		if e = m.bootLink(n, true); e != nil {
			return e
		}
		enabled = append(enabled, n)
	}
	if e := run("systemctl", "daemon-reload"); e != nil {
		return e
	}
	for _, n := range enabled {
		if !m.changed(old, n) && run("systemctl", "is-active", "--quiet", n) == nil {
			continue
		}
		if e := run("systemctl", "reset-failed", n); e != nil {
			return e
		}
		if e := run("systemctl", "restart", n); e != nil {
			return e
		}
	}
	if e := m.waitReady(enabled); e != nil {
		return e
	}
	if slices.Contains(enabled, tunnelUnit) {
		client := http.Client{Timeout: time.Second}
		for i := 0; i < 20; i++ {
			r, e := client.Get("http://127.0.0.1:19782/ready")
			if e == nil {
				r.Body.Close()
				if r.StatusCode == 200 {
					return nil
				}
			}
			time.Sleep(time.Second)
		}
		return fmt.Errorf("tunnel did not connect")
	}
	return nil
}
func (m Manager) Recover() error {
	journal := filepath.Join(m.Dir, "transaction")
	b, e := os.ReadFile(journal)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if e = m.switchTo(string(b)); e != nil {
		return e
	}
	if e = m.reconcile(""); e != nil {
		return fmt.Errorf("rollback pending: %w", e)
	}
	return os.Remove(journal)
}
func (m Manager) Apply(s model.State) error {
	if e := s.Validate(); e != nil {
		return e
	}
	if m.Live {
		if e := m.checkLinks(); e != nil {
			return e
		}
	}
	if m.Live {
		if e := preflightPorts(s); e != nil {
			return e
		}
	}
	core, cloud, e := m.binaries(s)
	if e != nil {
		return e
	}
	if core == "" {
		return fmt.Errorf("workspace mode requires --core /path/to/sing-box")
	}
	v, e := exec.Command(core, "version").Output()
	if e != nil || !compatible.Match(v) {
		return fmt.Errorf("requires stable sing-box 1.14.x")
	}
	files, e := Files(s, core, cloud)
	if e != nil {
		return e
	}
	if m.Live && (s.PublishHost != "" || s.ScheduleRules) {
		worker, e := installWorker()
		if e != nil {
			return e
		}
		if s.ScheduleRules {
			files[rulesService] = bytes.Replace(files[rulesService], []byte("/usr/local/bin/singforge"), []byte(worker), 1)
		}
		if s.PublishHost != "" {
			files[subUnit] = bytes.Replace(files[subUnit], []byte(Bin+"/singforge serve"), []byte(worker+" serve"), 1)
		}
	}
	if !m.Live {
		if e = markWorkspace(m.Dir); e != nil {
			return e
		}
	}
	gen, e := os.MkdirTemp(m.Dir, "gen-")
	if e != nil {
		return e
	}
	good := false
	defer func() {
		if !good {
			os.RemoveAll(gen)
		}
	}()
	for n, d := range files {
		if e = store.Write(filepath.Join(gen, n), d, 0600); e != nil {
			return e
		}
	}
	if e = run(core, "check", "-c", filepath.Join(gen, "server.json")); e != nil {
		return fmt.Errorf("core rejected candidate: %w", e)
	}
	if d, ok := files["nginx.conf"]; ok {
		ng, e := exec.LookPath("nginx")
		if e != nil {
			return fmt.Errorf("install nginx for HTTPS origin validation")
		}
		test := bytes.ReplaceAll(d, []byte("/run/credentials/"+edgeUnit+"/"), []byte(""))
		checkDir, e := os.MkdirTemp(m.Dir, ".nginx-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(checkDir)
		test = bytes.ReplaceAll(test, []byte("/run/singforge-edge/"), []byte(""))
		for _, n := range []string{"origin.crt", "origin.key", "subscription.crt", "subscription.key"} {
			if cert, ok := files[n]; ok {
				if e = store.Write(filepath.Join(checkDir, n), cert, 0600); e != nil {
					return e
				}
			}
		}
		path := filepath.Join(checkDir, "nginx-check.conf")
		if e = store.Write(path, test, 0600); e != nil {
			return e
		}
		if e = run(ng, "-t", "-p", checkDir, "-c", path); e != nil {
			return fmt.Errorf("nginx rejected candidate: %w", e)
		}
		os.Remove(path)
	}
	if _, ok := files[tunnelUnit]; ok && (m.Live || s.CloudDigest != "") {
		if e = run(cloud, "tunnel", "--config", filepath.Join(gen, "tunnel.yml"), "ingress", "validate"); e != nil {
			return e
		}
	}
	old, e := os.Readlink(filepath.Join(m.Dir, "current"))
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	journal := filepath.Join(m.Dir, "transaction")
	if e = store.Write(journal, []byte(old), 0600); e != nil {
		return e
	}
	if e = m.switchTo(filepath.Base(gen)); e == nil {
		e = m.reconcile(old)
	}
	if e != nil {
		rollback := m.Recover()
		if rollback != nil {
			good = true
		}
		return errors.Join(e, rollback)
	}
	good = true
	if e = os.Remove(journal); e != nil {
		return e
	}
	if e = store.Sync(m.Dir); e != nil {
		return e
	}
	if old != "" {
		return removeGeneration(m.Dir, old)
	}
	return nil
}
func (m Manager) Service(action string) error {
	if !m.Live {
		return fmt.Errorf("workspace has no systemd services")
	}
	if !slices.Contains([]string{"start", "stop", "restart", "status", "logs", "reload"}, action) {
		return fmt.Errorf("unknown service action")
	}
	args := []string{}
	for _, n := range all {
		if n == rulesService && action != "logs" {
			continue
		}
		if _, e := os.Stat(filepath.Join(m.Dir, "current", n)); e == nil {
			args = append(args, n)
		}
	}
	if len(args) == 0 {
		return fmt.Errorf("no managed units")
	}
	var cmd *exec.Cmd
	if action == "logs" {
		a := []string{"--no-pager", "-n", "80"}
		for _, n := range args {
			a = append(a, "-u", n)
		}
		cmd = exec.Command("journalctl", a...)
	} else {
		if action == "reload" {
			action = "reload-or-restart"
		}
		if action == "status" {
			action = "is-active"
		}
		cmd = exec.Command("systemctl", append([]string{action}, args...)...)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
func (m Manager) Uninstall(purge bool) error {
	gen, e := os.Readlink(filepath.Join(m.Dir, "current"))
	if e != nil {
		return e
	}
	if filepath.Base(gen) != gen || !strings.HasPrefix(gen, "gen-") {
		return fmt.Errorf("invalid generation")
	}
	if m.Live {
		if e = m.checkLinks(); e != nil {
			return e
		}
		for _, n := range all {
			if e = m.detach(n); e != nil {
				return e
			}
		}
		if e = run("systemctl", "daemon-reload"); e != nil {
			return e
		}
	}
	if m.Live {
		if _, e = Firewall(m.Dir, model.State{}, true, false, true); e != nil {
			return e
		}
		if _, e = Tune(m.Dir, "disable", "", false, true); e != nil {
			return e
		}
	}
	if m.Live {
		if e = RemoveRenewalHook(); e != nil {
			return e
		}
	}
	if !purge {
		return nil
	}
	if !m.Live {
		if e = markWorkspace(m.Dir); e != nil {
			return e
		}
	}
	if e = os.Remove(filepath.Join(m.Dir, "current")); e != nil {
		return e
	}
	for _, name := range []string{"cloudflare.token", "created-tunnel.json"} {
		if e = os.Remove(filepath.Join(m.Dir, name)); e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	return removeGeneration(m.Dir, gen)
}
func removeGeneration(dir, gen string) error {
	if filepath.Base(gen) != gen || !strings.HasPrefix(gen, "gen-") {
		return fmt.Errorf("invalid generation name")
	}
	info, e := os.Lstat(filepath.Join(dir, gen))
	if e != nil {
		return e
	}
	if !info.IsDir() {
		return fmt.Errorf("invalid generation directory")
	}
	unknown := false
	entries, e := os.ReadDir(filepath.Join(dir, gen))
	if e != nil {
		return e
	}
	for _, f := range entries {
		if !generationFile(f.Name()) {
			unknown = true
			continue
		}
		if e = os.Remove(filepath.Join(dir, gen, f.Name())); e != nil {
			return e
		}
	}
	if unknown {
		return nil
	}
	return os.Remove(filepath.Join(dir, gen))
}

func generationFile(name string) bool {
	return slices.Contains([]string{"server.json", "state.json", "renewal-lineage", "bundle.json", "nginx.conf", "origin.key", "origin.crt", "subscription.key", "subscription.crt", "tunnel.json", "tunnel.yml", "connector.token"}, name) || slices.Contains(all, name)
}
