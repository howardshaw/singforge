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
	case "ips", "domains":
	default:
		return fmt.Errorf("choose --mode default, domains or ips")
	}
	if len(p.Addresses) == 0 || len(p.Addresses) > 8 {
		return fmt.Errorf("select one to eight preferred edge addresses")
	}
	for i, h := range p.Addresses {
		if slices.Contains(p.Addresses[:i], h) {
			return fmt.Errorf("duplicate preferred edge address")
		}
		if p.Mode == "ips" && !edge.ValidIP(h) {
			return fmt.Errorf("expected an individual Cloudflare IP within its published ranges")
		}
		if p.Mode == "domains" && !Domain(h) {
			return fmt.Errorf("expected a preferred DNS hostname")
		}
	}
	return nil
}
