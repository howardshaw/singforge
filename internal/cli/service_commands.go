package cli

import (
	"fmt"
	"github.com/howardshaw/singforge/internal/host"
	"os/exec"
	"path/filepath"
	"strings"
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
	core, err := m.CorePath(s)
	valid := err == nil && exec.Command(core, "check", "-c", filepath.Join(m.Dir, "current/server.json")).Run() == nil
	checks := []host.ServiceCheck{}
	if m.Live {
		checks = m.Checks(s)
	}
	healthy := valid && (!m.Live || len(checks) > 0)
	for _, c := range checks {
		healthy = healthy && c.Error == ""
	}
	if op.options.json {
		if e := printJSON(map[string]any{"configuration_valid": valid, "healthy": healthy, "services": checks}); e != nil {
			return e
		}
	} else {
		label := "valid"
		if !valid {
			label = "failed"
		}
		fmt.Println("sing-box configuration  ·  " + label)
		for _, c := range checks {
			fmt.Printf("\n%s  ·  %s (%s)\n", c.Unit, c.State, c.SubState)
			fmt.Printf("  Boot: %s   PID: %d   Restarts: %d\n", c.Boot, c.PID, c.Restarts)
			if len(c.Listeners) > 0 {
				fmt.Println("  Listening: " + strings.Join(c.Listeners, ", "))
			}
			if len(c.Missing) > 0 {
				fmt.Println("  Missing: " + strings.Join(c.Missing, ", "))
			}
			if c.ConnectorReady != nil {
				fmt.Printf("  Cloudflare connector ready: %t\n", *c.ConnectorReady)
			}
			if c.Error != "" {
				fmt.Println("  " + c.Error)
			}
		}
	}
	if !healthy {
		return fmt.Errorf("one or more health checks failed")
	}
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
