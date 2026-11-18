package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

func (op operation) install() error {
	o := op.options
	m := op.manager
	s := op.state
	exists := op.installed
	var e error
	if exists {
		if o.dry {
			return printJSON(s.Endpoints)
		}
		return m.Apply(s)
	}
	e = install(&s, o, exists)
	if e != nil {
		return e
	}
	return op.commit(s, "")
}
func (op operation) status() error {
	s := op.state
	summary := map[string]any{"installed": true, "endpoints": s.Endpoints, "publication": s.PublishHost, "dns": s.DNS}
	if op.manager.Live {
		states, e := op.manager.ServiceStates()
		if e != nil {
			return e
		}
		summary["services"] = states
	}
	return printJSON(summary)

}
func (op operation) doctor() error {
	m := op.manager
	s := op.state
	var e error
	core, err := m.CorePath(s)
	if err != nil {
		return err
	}
	if e = exec.Command(core, "check", "-c", filepath.Join(m.Dir, "current/server.json")).Run(); e != nil {
		return fmt.Errorf("core validation failed")
	}
	if m.Live {
		return m.Health()
	}
	fmt.Println("workspace configuration validated")
	return nil
}
func (op operation) start() error {
	o := op.options
	m := op.manager
	cmd := op.path
	if o.dry {
		return nil
	}
	return m.Service(cmd)
}
