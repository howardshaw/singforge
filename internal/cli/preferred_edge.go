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
		if domains != "" && mode != "domains" && mode != "all" || ips != "" && mode != "ips" && mode != "all" {
			return fmt.Errorf("--domains requires domains or all mode; --ips requires ips or all mode")
		}
		p := model.EdgePreferences{Mode: mode}
		if mode == "domains" || mode == "all" {
			addrs, err := edgeList("domains", domains, edge.Community)
			if err != nil {
				return err
			}
			p.Addresses = append(p.Addresses, addrs...)
		}
		if mode == "ips" || mode == "all" {
			addrs, err := edgeList("ips", ips, edge.DefaultIPs)
			if err != nil {
				return err
			}
			p.Addresses = append(p.Addresses, addrs...)
		}
		if mode == "" {
			return fmt.Errorf("choose an edge mode")
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

func edgeList(kind, raw string, defaults []string) ([]string, error) {
	if raw == "" {
		return defaults, nil
	}
	list := strings.Split(raw, ",")
	for i, value := range list {
		h := strings.ToLower(strings.TrimSpace(value))
		if ip, err := netip.ParseAddr(h); kind == "ips" && err == nil {
			h = ip.String()
		}
		list[i] = h
	}
	return list, (model.EdgePreferences{Mode: kind, Addresses: list}).Validate()
}
