// Package edge manages alternative dial addresses for Cloudflare WebSocket proxies.
package edge

import (
	"fmt"
	"maps"
)

const Group = "Preferred Edge"

var Community = []string{
	"cf.090227.xyz", "cloudflare.182682.xyz", "bestcf.top", "cdn.2020111.xyz",
	"cf.0sm.com", "cf.877771.xyz", "cf.zhetengsha.eu.org", "cfip.cfcdn.vip",
}

type Proxy = map[string]any

// Variants keeps the endpoint's TLS identity, certificate checks, WS Host,
// path, port and credentials. Only the dial destination and display name vary.
func Variants(bases []Proxy, addresses []string) ([]Proxy, []string) {
	var variants []Proxy
	var priority []string
	for _, addr := range addresses {
		for _, base := range bases {
			if addr == base["server"] {
				continue
			}
			name := fmt.Sprintf("%s / %s", base["name"], addr)
			p := maps.Clone(base)
			p["name"], p["server"] = name, addr
			variants = append(variants, p)
			priority = append(priority, name)
		}
	}
	for _, base := range bases {
		priority = append(priority, base["name"].(string))
	}
	return variants, priority
}
