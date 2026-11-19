package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Rule struct{ Kind, Value, Group string }
type Snapshot struct {
	Version string
	Rules   []Rule
}
type Source struct{ File, Group, Digest, Kind string }

const Release = "0b135fd25e104e333a55e75d48ea876d25ce0658"
const PolicyRevision = "ai-domestic-geodata-v3"
const Base = "https://raw.githubusercontent.com/Loyalsoldier/clash-rules/" + Release + "/"

var Sources = []Source{{"gfw.txt", "Main", "3196009dbb1619722c05b791cab8d40dc1f6252d0be5273ac00a985c8ebb542d", "domain"}}

var domain = regexp.MustCompile(`^[a-z0-9_]([a-z0-9_\-]{0,61}[a-z0-9_])?(\.[a-z0-9_]([a-z0-9_\-]{0,61}[a-z0-9_])?)*$`)

func (r Rule) Check() error {
	if !slices.Contains([]string{"Main", "AI", "AI-CN", "Streaming", "Communication", "DIRECT", "REJECT"}, r.Group) {
		return fmt.Errorf("invalid routing group")
	}
	switch r.Kind {
	case "DOMAIN-KEYWORD":
		if len(r.Value) > 0 && len(r.Value) < 254 && strings.Trim(r.Value, "abcdefghijklmnopqrstuvwxyz0123456789.-_") == "" {
			return nil
		}
	case "DOMAIN", "DOMAIN-SUFFIX":
		if len(r.Value) <= 253 && domain.MatchString(r.Value) {
			return nil
		}
	case "IP-CIDR", "IP-CIDR6":
		if ip, e := netip.ParsePrefix(r.Value); e == nil && (r.Kind == "IP-CIDR6") == ip.Addr().Is6() {
			return nil
		}
	}
	return fmt.Errorf("invalid %s rule", r.Kind)
}
func (r Rule) Match(v string) bool {
	switch r.Kind {
	case "DOMAIN-KEYWORD":
		return strings.Contains(v, r.Value)
	case "DOMAIN":
		return r.Value == v
	case "DOMAIN-SUFFIX":
		return r.Value == v || strings.HasSuffix(v, "."+r.Value)
	default:
		p, e := netip.ParsePrefix(r.Value)
		ip, err := netip.ParseAddr(v)
		return e == nil && err == nil && p.Contains(ip)
	}
}
func Load(src Source, b []byte) ([]Rule, error) {
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != src.Digest {
		return nil, fmt.Errorf("%s digest mismatch", src.File)
	}
	var doc struct{ Payload []string }
	if e := yaml.Unmarshal(b, &doc); e != nil || len(doc.Payload) == 0 {
		return nil, fmt.Errorf("invalid rule payload")
	}
	r := make([]Rule, 0, len(doc.Payload))
	for _, v := range doc.Payload {
		kind := "DOMAIN"
		if src.Kind == "classical" {
			parts := strings.Split(v, ",")
			if len(parts) < 2 || len(parts) > 3 {
				return nil, fmt.Errorf("malformed classical rule")
			}
			kind = parts[0]
			v = strings.ToLower(parts[1])
			if kind == "IP-ASN" || kind == "PROCESS-NAME" {
				continue
			}
		} else if strings.HasPrefix(v, "+.") {
			kind = "DOMAIN-SUFFIX"
			v = v[2:]
		} else if src.Kind == "cidr" {
			kind = "IP-CIDR"
			if strings.Contains(v, ":") {
				kind = "IP-CIDR6"
			}
		}
		a := Rule{kind, v, src.Group}
		if e := a.Check(); e != nil {
			return nil, e
		}
		r = append(r, a)
	}
	return r, nil
}
func Update() (Snapshot, error) {
	snap := Snapshot{Version: Release + "/" + PolicyRevision}
	c := http.Client{Timeout: 30 * time.Second}
	for _, src := range Sources {
		u := Base + src.File
		r, e := c.Get(u)
		if e != nil {
			return Snapshot{}, e
		}
		b, e := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		r.Body.Close()
		if e != nil || r.StatusCode != 200 {
			return Snapshot{}, fmt.Errorf("rule download failed")
		}
		v, e := Load(src, b)
		if e != nil {
			return Snapshot{}, e
		}
		snap.Rules = append(snap.Rules, v...)
	}
	return snap, nil
}
