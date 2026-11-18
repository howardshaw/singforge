package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/howardshaw/singforge/internal/cloudflare"
	"github.com/howardshaw/singforge/internal/host"
	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/store"
)

func cloudCommand(cmd string, s *model.State, m host.Manager, o opts) (bool, bool, error) {
	if !strings.HasPrefix(cmd, "cloudflare ") && cmd != "domain sync" {
		return false, false, nil
	}
	path := filepath.Join(m.Dir, "cloudflare.token")
	if cmd == "cloudflare configure" {
		path = o.get("token-file")
	}
	token, e := os.ReadFile(path)
	if e != nil {
		return true, false, fmt.Errorf("configure a scoped API token using --token-file")
	}
	c := cloudflare.New(strings.TrimSpace(string(token)))
	if cmd == "cloudflare configure" {
		z, e := c.Zone(o.get("domain"))
		if e != nil {
			return true, false, e
		}
		s.Zone = z.Name
		s.ZoneID = z.ID
		s.AccountID = z.Account.ID
		if !o.dry {
			e = store.Write(filepath.Join(m.Dir, "cloudflare.token"), token, 0600)
		}
		return true, true, e
	}
	if s.ZoneID == "" {
		return true, false, fmt.Errorf("run cloudflare configure --domain ZONE --token-file FILE first")
	}
	switch cmd {
	case "cloudflare tunnel list":
		t, e := c.Tunnels(s.AccountID)
		if e == nil {
			e = printJSON(t)
		}
		return true, false, e
	case "cloudflare tunnel create":
		if s.TunnelCredentials != "" {
			return true, false, fmt.Errorf("a tunnel is already configured")
		}
		if o.dry {
			return true, false, nil
		}
		secret := base64.StdEncoding.EncodeToString(model.Random(32))
		t, e := c.Create(s.AccountID, o.get("name"), secret)
		if e != nil {
			return true, false, e
		}
		raw, _ := json.Marshal(map[string]string{"AccountTag": s.AccountID, "TunnelSecret": secret, "TunnelID": t.ID})
		s.TunnelCredentials = string(raw)
		s.TunnelID, e = host.TunnelID(string(raw))
		if e == nil {
			e = store.Write(filepath.Join(m.Dir, "created-tunnel.json"), raw, 0600)
		}
		return true, true, e
	case "cloudflare dns cleanup":
		if !o.yes && !o.dry {
			return true, false, fmt.Errorf("confirm DNS deletion with --yes")
		}
		path := filepath.Join(m.Dir, "dns-owned.json")
		b, e := os.ReadFile(path)
		if os.IsNotExist(e) {
			return true, false, nil
		}
		if e != nil {
			return true, false, e
		}
		owned := map[string]string{}
		if e = json.Unmarshal(b, &owned); e != nil {
			return true, false, e
		}
		for key, id := range owned {
			p := strings.Split(key, "/")
			if len(p) != 3 || p[0] != s.ZoneID {
				continue
			}
			fmt.Println(p[1], p[2], id)
			if e = c.DeleteOwned(p[0], p[1], p[2], id, o.dry); e != nil {
				return true, false, e
			}
			if !o.dry {
				delete(owned, key)
				b, e = json.Marshal(owned)
				if e == nil {
					e = store.Write(path, b, 0600)
				}
				if e != nil {
					return true, false, e
				}
			}
		}
		return true, false, nil
	case "cloudflare dns list":
		r, e := c.Records(s.ZoneID, "")
		if e == nil {
			e = printJSON(r)
		}
		return true, false, e
	case "cloudflare dns sync", "domain sync":
		var wanted []cloudflare.Record
		for _, h := range []string{s.Direct, s.CDN, s.PublishHost} {
			if !model.Domain(h) {
				continue
			}
			for _, a := range strings.Split(o.get("address"), ",") {
				ip := net.ParseIP(a)
				if ip == nil {
					return true, false, fmt.Errorf("provide --address IPv4[,IPv6]")
				}
				typ := "AAAA"
				if ip.To4() != nil {
					typ = "A"
				}
				wanted = append(wanted, cloudflare.Record{Type: typ, Name: h, Content: ip.String(), Proxied: h == s.CDN})
			}
		}
		if s.Tunnel != "" {
			wanted = append(wanted, cloudflare.Record{Type: "CNAME", Name: s.Tunnel, Content: s.TunnelID + ".cfargotunnel.com", Proxied: true})
		}
		ledger := filepath.Join(m.Dir, "dns-owned.json")
		owned := map[string]string{}
		b, e := os.ReadFile(ledger)
		if e == nil {
			if e = json.Unmarshal(b, &owned); e != nil {
				return true, false, e
			}
		} else if !os.IsNotExist(e) {
			return true, false, e
		}
		z := cloudflare.Zone{ID: s.ZoneID, Name: s.Zone}
		for _, plan := range []bool{true, false} {
			if !plan && o.dry {
				break
			}
			for _, r := range wanted {
				k := z.ID + "/" + r.Name + "/" + r.Type
				id, e := c.Sync(z, r, owned[k], o.get("replace-id"), plan)
				if e != nil {
					return true, false, e
				}
				if !plan && id != "" {
					owned[k] = id
					b, e = json.Marshal(owned)
					if e == nil {
						e = store.Write(ledger, b, 0600)
					}
					if e != nil {
						return true, false, e
					}
				}
			}
		}
		return true, false, printJSON(wanted)
	default:
		return false, false, nil
	}
}
