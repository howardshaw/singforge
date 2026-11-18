package cli

import (
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/howardshaw/singforge/internal/model"
)

func (op operation) cloudflareEnable() error {
	o := op.options
	s := op.state
	e := Edge(&s, o.get("mode"), o.get("host"), o)
	if e != nil {
		return e
	}
	return op.commit(s, "")
}
func (op operation) cloudflareDisable() error {
	o := op.options
	s := op.state
	if !o.yes && !o.dry {
		return fmt.Errorf("confirm with --yes")
	}
	mode := o.get("mode")
	switch mode {
	case "cdn":
		s.CDN = ""
		s.OriginCert = ""
		s.OriginKey = ""
	case "tunnel":
		s.Tunnel = ""
	default:
		return fmt.Errorf("expected cdn or tunnel")
	}
	s.Endpoints = slices.DeleteFunc(s.Endpoints, func(v model.Endpoint) bool { return v.Mode == mode })
	return op.commit(s, "")
}
func (op operation) domainStatus() error {
	s := op.state
	return printJSON(map[string]string{"zone": s.Zone, "direct": s.Direct, "cdn": s.CDN, "tunnel": s.Tunnel})
}
func (op operation) domainConfigure() error {
	o := op.options
	s := op.state
	if o.get("mode") != "dns-only" || !model.Domain(o.get("domain")) || !model.Host(o.get("hostname")) {
		return fmt.Errorf("domain configure requires --domain, --hostname and --mode dns-only")
	}
	if s.Zone != o.get("domain") {
		s.ZoneID = ""
		s.AccountID = ""
	}
	s.Zone = o.get("domain")
	previous := s.Direct
	s.Direct = o.get("hostname") + "." + s.Zone
	for i := range s.Endpoints {
		if s.Endpoints[i].Mode == "direct" && s.Endpoints[i].Host == previous {
			s.Endpoints[i].Host = s.Direct
		}
	}
	return op.commit(s, "")
}
func (op operation) cloudAPI() error {
	o := op.options
	m := op.manager
	s := op.state
	cmd := op.path
	handled, save, err := cloudCommand(cmd, &s, m, o)
	if err != nil {
		return err
	}
	if !handled {
		return fmt.Errorf("unknown command %q; use singforge help", cmd)
	}
	if !save {
		return nil
	}
	return op.commit(s, "")
}

func (op operation) tunnelRoutes() error {
	rows := []map[string]string{}
	for _, n := range op.state.Nodes() {
		if n.Mode == "tunnel" {
			rows = append(rows, map[string]string{"hostname": n.Host, "path": "^" + n.Auth.Path + "$", "service": fmt.Sprintf("http://127.0.0.1:%d", n.Auth.Port)})
		}
	}
	return printJSON(rows)
}

func (op operation) tunnelStatus() error {
	s := op.state
	report := map[string]any{"enabled": s.Tunnel != "", "hostname": s.Tunnel, "id": s.TunnelID}
	if s.Tunnel != "" {
		mgmt := "local"
		if s.ConnectorToken != "" {
			mgmt = "dashboard"
		}
		report["management"] = mgmt
		if op.manager.Live {
			client := http.Client{Timeout: 3 * time.Second}
			resp, e := client.Get("http://127.0.0.1:19782/ready")
			ready := false
			if e == nil {
				resp.Body.Close()
				ready = resp.StatusCode == 200
			}
			report["ready"] = ready
			if !ready {
				if e := printJSON(report); e != nil {
					return e
				}
				return fmt.Errorf("connector is not ready")
			}
		}
	}
	return printJSON(report)
}
