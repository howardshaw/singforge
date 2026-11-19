package model

import (
	"fmt"
	"slices"

	"github.com/howardshaw/singforge/internal/edge"
)

type EdgePreferences struct {
	Mode      string
	Addresses []string
}

func (p EdgePreferences) Validate() error {
	switch p.Mode {
	case "", "default":
		if len(p.Addresses) > 0 {
			return fmt.Errorf("default edge mode uses the endpoint's own hostname")
		}
		return nil
	case "ips", "domains", "all":
	default:
		return fmt.Errorf("choose --mode default, domains, ips or all")
	}
	if len(p.Addresses) == 0 {
		return fmt.Errorf("select at least one preferred edge address")
	}
	domains, ips := 0, 0
	for i, h := range p.Addresses {
		if slices.Contains(p.Addresses[:i], h) {
			return fmt.Errorf("duplicate preferred edge address")
		}
		switch {
		case Domain(h) && p.Mode != "ips":
			domains++
		case edge.ValidIP(h) && p.Mode != "domains":
			ips++
		default:
			return fmt.Errorf("invalid %s edge address %q", p.Mode, h)
		}
	}
	if domains > 8 || ips > 8 {
		return fmt.Errorf("choose at most eight domains and eight Cloudflare IPs")
	}
	return nil
}
