package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/howardshaw/singforge/internal/host"
	"github.com/howardshaw/singforge/internal/routing"
	"github.com/howardshaw/singforge/internal/store"
)

func (op operation) bbrStatus() error {
	o := op.options
	m := op.manager
	cmd := op.path
	if !m.Live {
		return fmt.Errorf("kernel tuning requires the deployment host")
	}
	v, err := host.Tune(m.Dir, strings.TrimPrefix(cmd, "bbr "), o.get("profile"), o.dry, o.yes)
	if v != nil {
		if e := printJSON(v); e != nil {
			return e
		}
	}
	return err
}
func (op operation) coreUpdate() error {
	o := op.options
	m := op.manager
	s := op.state
	var e error
	dir, e := os.MkdirTemp(m.Dir, ".update-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	asset, e := op.release(dir, "sing-box")
	if e != nil {
		return e
	}
	if e = m.StageCore(&s, asset.File, asset.Digest, o.dry); e != nil {
		return e
	}
	if e != nil {
		return e
	}
	return op.commit(s, "")
}
func (op operation) coreRollback() error {
	o := op.options
	s := op.state
	if !o.yes && !o.dry {
		return fmt.Errorf("confirm with --yes")
	}
	s.CoreDigest, s.PreviousCoreDigest = s.PreviousCoreDigest, s.CoreDigest
	return op.commit(s, "")
}
func (op operation) selfUpdate() error {
	o := op.options
	m := op.manager
	cmd := op.path
	if !o.yes && !o.dry {
		return fmt.Errorf("confirm with --yes")
	}
	if cmd == "self rollback" {
		return m.SelfUpdate("", "", true, o.dry)
	}
	dir, e := os.MkdirTemp(m.Dir, ".update-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	a, e := op.release(dir, "singforge")
	if e != nil {
		return e
	}
	return m.SelfUpdate(a.File, a.Digest, false, o.dry)
}
func (op operation) backupCreate() error {
	o := op.options
	s := op.state
	if o.dry {
		return nil
	}
	return store.Backup(s, o.get("file"))
}
func (op operation) backupRestore() error {
	o := op.options
	if !o.yes && !o.dry {
		return fmt.Errorf("restore requires --yes")
	}
	s, e := store.Restore(o.get("file"))
	if e != nil {
		return e
	}
	// Backups can predate revocation. Restoring node credentials must not revive grants.
	s.Grants = nil
	return op.commit(s, "")
}
func (op operation) uninstall() error {
	o := op.options
	m := op.manager
	s := op.state
	if !o.yes && !o.dry {
		return fmt.Errorf("uninstall requires --yes")
	}
	if o.dry {
		return printJSON(s.Endpoints)
	}
	return m.Uninstall(o.purge)
}
func (op operation) cloudflaredUpdate() error {
	s := op.state
	o := op.options
	if s.Tunnel == "" {
		return fmt.Errorf("enable a named tunnel first")
	}
	dir, e := os.MkdirTemp(op.manager.Dir, ".update-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	asset, e := op.release(dir, "cloudflared")
	if e != nil {
		return e
	}
	if e := op.manager.StageConnector(&s, asset.File, asset.Digest, o.dry); e != nil {
		return e
	}
	return op.commit(s, "")
}
func (op operation) cloudflaredRollback() error {
	s := op.state
	if !op.options.yes && !op.options.dry {
		return fmt.Errorf("confirm with --yes")
	}
	s.CloudDigest, s.PreviousCloudDigest = s.PreviousCloudDigest, s.CloudDigest
	return op.commit(s, "")
}

func (op operation) release(dir, component string) (host.Release, error) {
	file, digest := op.options.get("file"), op.options.get("sha256")
	if file != "" || digest != "" {
		if file == "" || digest == "" {
			return host.Release{}, fmt.Errorf("--file and --sha256 must be supplied together")
		}
		return host.Release{File: file, Digest: digest}, nil
	}
	if !op.manager.Live {
		return host.Release{}, fmt.Errorf("workspace updates require --file and --sha256")
	}
	return host.FetchRelease(dir, component)
}
func (op operation) updateAll() error {
	if !op.options.all {
		return fmt.Errorf("select --all or run a component update")
	}
	if !op.manager.Live {
		return fmt.Errorf("combined updates require a deployed Linux host")
	}
	if !op.options.yes && !op.options.dry {
		return fmt.Errorf("confirm CLI replacement with --yes")
	}
	components := []string{"sing-box", "singforge"}
	if op.state.Tunnel != "" {
		components = append(components, "cloudflared")
	}
	if op.options.dry {
		return printJSON(components)
	}
	dir, e := os.MkdirTemp(op.manager.Dir, ".update-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	assets := map[string]host.Release{}
	for _, c := range components {
		assets[c], e = host.FetchRelease(dir, c)
		if e != nil {
			return e
		}
	}
	s := op.state
	snap, e := routing.Update()
	if e != nil {
		return e
	}
	if s.Rules.Version != snap.Version {
		s.PreviousRules = s.Rules
		s.Rules = snap
	}
	c := assets["sing-box"]
	if e = op.manager.StageCore(&s, c.File, c.Digest, false); e != nil {
		return e
	}
	if cf, ok := assets["cloudflared"]; ok {
		if e = op.manager.StageConnector(&s, cf.File, cf.Digest, false); e != nil {
			return e
		}
	}
	if e = op.manager.Apply(s); e != nil {
		return e
	}
	cli := assets["singforge"]
	if e = op.manager.SelfUpdate(cli.File, cli.Digest, false, false); e != nil {
		return errors.Join(e, op.manager.Apply(op.state))
	}
	fmt.Println("updated")
	return nil
}

func (op operation) firewallSync() error {
	if !op.manager.Live {
		return fmt.Errorf("firewall management requires the deployment host")
	}
	ports, e := host.Firewall(op.manager.Dir, op.state, false, op.options.dry, op.options.yes)
	if e != nil {
		return e
	}
	return printJSON(ports)
}
