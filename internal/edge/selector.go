// Package edge manages alternative dial addresses for Cloudflare WebSocket proxies.
package edge

import (
	"fmt"
	"maps"
)

const Group = "Preferred Edge"

var Community = []string{
	"cf.090227.xyz", "cloudflare.182682.xyz", "cdn.2020111.xyz",
	"cf.0sm.com", "cf.877771.xyz",
}

type Proxy = map[string]any

// Variants keeps the endpoint's TLS identity, certificate checks, WS Host,
// path, port and credentials. Only the dial destination and display name vary.
func Variants(base Proxy, addresses []string) []Proxy {
	var variants []Proxy
	for _, addr := range addresses {
		if addr == base["server"] {
			continue
		}
		name := fmt.Sprintf("%s / %s", base["name"], addr)
		p := maps.Clone(base)
		p["name"], p["server"] = name, addr
		variants = append(variants, p)
	}
	return variants
}
