package cli

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/howardshaw/singforge/internal/edge"
	"github.com/howardshaw/singforge/internal/model"
)

func (op operation) preferredEdge() error {
	s := op.state
	var baselines []string
	for _, n := range s.Nodes() {
		if n.Spec.WS && (n.Mode == "cdn" || n.Mode == "tunnel") && !slices.Contains(baselines, n.Host) {
			baselines = append(baselines, n.Host)
		}
	}
	if op.path == "edge set" {
		mode := op.options.get("mode")
		domains, ips := op.options.get("domains"), op.options.get("ips")
		if domains != "" && mode != "domains" || ips != "" && mode != "ips" {
			return fmt.Errorf("use --domains with --mode domains or --ips with --mode ips")
		}
		p := model.EdgePreferences{Mode: mode}
		var list string
		switch mode {
		case "default":
		case "domains":
			list = domains
			p.Addresses = slices.Clone(edge.Community)
		case "ips":
			list = ips
			p.Addresses = slices.Clone(edge.DefaultIPs)
		default:
			return fmt.Errorf("choose --mode default, domains or ips")
		}
		if list != "" {
			p.Addresses = strings.Split(list, ",")
			for i, h := range p.Addresses {
				h = strings.ToLower(strings.TrimSpace(h))
				if ip, err := netip.ParseAddr(h); mode == "ips" && err == nil {
					h = ip.String()
				}
				p.Addresses[i] = h
			}
		}
		if err := p.Validate(); err != nil {
			return err
		}
		if len(p.Addresses) > 0 && len(baselines) == 0 {
			return fmt.Errorf("enable a CDN or Tunnel WebSocket endpoint first")
		}
		s.PreferredEdge = p
		if !op.options.dry {
			return op.commit(s, "")
		}
	}
	mode := s.PreferredEdge.Mode
	if mode == "" {
		mode = "default"
	}
	return printJSON(map[string]any{"mode": mode, "baselines": baselines, "preferred_addresses": s.PreferredEdge.Addresses, "community_domains": edge.Community, "candidate_ips": edge.DefaultIPs})
}
