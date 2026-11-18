// Package cloudflare reconciles individual records against an explicitly selected zone.
package cloudflare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/howardshaw/singforge/internal/model"
)

type Client struct {
	Base, Token string
	HTTP        *http.Client
}

func New(token string) Client {
	return Client{"https://api.cloudflare.com/client/v4", token, &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

type response struct {
	Success    bool
	Result     json.RawMessage
	ResultInfo struct {
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

func (c Client) call(method, path string, body, out any) (int, error) {
	var r io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return 0, e
		}
		r = bytes.NewReader(b)
	}
	req, e := http.NewRequest(method, c.Base+path, r)
	if e != nil {
		return 0, e
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	res, e := c.HTTP.Do(req)
	if e != nil {
		return 0, fmt.Errorf("could not reach Cloudflare")
	}
	defer res.Body.Close()
	var v response
	if res.StatusCode/100 != 2 || json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&v) != nil || !v.Success {
		return 0, fmt.Errorf("request rejected by Cloudflare: %s (HTTP %d)", method, res.StatusCode)
	}
	if out != nil {
		e = json.Unmarshal(v.Result, out)
	}
	return v.ResultInfo.TotalPages, e
}

var ident = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)

type Zone struct {
	ID, Name string
	Account  struct{ ID string }
}

func (c Client) Zone(name string) (Zone, error) {
	var z []Zone
	if !model.Domain(name) {
		return Zone{}, fmt.Errorf("invalid zone")
	}
	_, e := c.call("GET", "/zones?name="+url.QueryEscape(name), nil, &z)
	if e != nil {
		return Zone{}, e
	}
	if len(z) != 1 || z[0].Name != name || !ident.MatchString(z[0].ID) || !ident.MatchString(z[0].Account.ID) {
		return Zone{}, fmt.Errorf("zone not found or ambiguous")
	}
	return z[0], nil
}

type Record struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

func (c Client) Records(zone, name string) ([]Record, error) {
	if !ident.MatchString(zone) {
		return nil, fmt.Errorf("invalid zone ID")
	}
	out := []Record{}
	for p := 1; ; p++ {
		var item []Record
		path := fmt.Sprintf("/zones/%s/dns_records?per_page=100&page=%d", zone, p)
		if name != "" {
			path += "&name=" + url.QueryEscape(name)
		}
		pages, e := c.call("GET", path, nil, &item)
		if e != nil {
			return nil, e
		}
		out = append(out, item...)
		if p >= pages {
			return out, nil
		}
	}
}
func (c Client) Sync(z Zone, w Record, owned, replace string, dry bool) (string, error) {
	if !model.Domain(w.Name) || (w.Name != z.Name && !strings.HasSuffix(w.Name, "."+z.Name)) {
		return "", fmt.Errorf("name outside zone")
	}
	ip := net.ParseIP(w.Content)
	switch w.Type {
	case "A":
		if ip.To4() == nil {
			return "", fmt.Errorf("invalid IPv4")
		}
	case "AAAA":
		if ip == nil || ip.To4() != nil {
			return "", fmt.Errorf("invalid IPv6")
		}
	case "CNAME":
		if !model.Domain(w.Content) {
			return "", fmt.Errorf("invalid CNAME")
		}
	default:
		return "", fmt.Errorf("unsupported DNS type")
	}
	records, e := c.Records(z.ID, w.Name)
	if e != nil {
		return "", e
	}
	var candidates []Record
	for _, a := range records {
		if a.Name != w.Name {
			continue
		}
		if a.Type != w.Type {
			if a.Type == "CNAME" || w.Type == "CNAME" {
				return "", fmt.Errorf("incompatible existing %s record %s", a.Type, a.ID)
			}
			continue
		}
		candidates = append(candidates, a)
	}
	base := "/zones/" + z.ID + "/dns_records"
	if len(candidates) > 1 {
		return "", fmt.Errorf("ambiguous record set; resolve explicitly in Cloudflare")
	}
	if len(candidates) == 1 {
		a := candidates[0]
		if !ident.MatchString(a.ID) {
			return "", fmt.Errorf("invalid record ID")
		}
		if a.Content == w.Content && a.Proxied == w.Proxied {
			return owned, nil
		}
		if a.ID != owned && a.ID != replace {
			return "", fmt.Errorf("conflict with existing %s %s (%s, proxied=%t); confirm using --replace-id %s", a.Type, a.Name, a.Content, a.Proxied, a.ID)
		}
		if dry {
			return owned, nil
		}
		_, e = c.call("PATCH", base+"/"+a.ID, w, nil)
		return owned, e
	}
	if dry {
		return "", nil
	}
	var a Record
	_, e = c.call("POST", base, w, &a)
	if e == nil && !ident.MatchString(a.ID) {
		e = fmt.Errorf("invalid created record ID")
	}
	return a.ID, e
}

type Tunnel struct{ ID, Name, Status string }

func (c Client) Tunnels(account string) ([]Tunnel, error) {
	if !ident.MatchString(account) {
		return nil, fmt.Errorf("invalid account")
	}
	var all []Tunnel
	for p := 1; ; p++ {
		var v []Tunnel
		pages, e := c.call("GET", fmt.Sprintf("/accounts/%s/cfd_tunnel?is_deleted=false&page=%d", account, p), nil, &v)
		if e != nil {
			return nil, e
		}
		all = append(all, v...)
		if p >= pages {
			return all, nil
		}
	}
}
func (c Client) Create(account, name, secret string) (Tunnel, error) {
	var t Tunnel
	if !ident.MatchString(account) || !model.Host(name) {
		return t, fmt.Errorf("invalid tunnel account or name")
	}
	_, e := c.call("POST", "/accounts/"+account+"/cfd_tunnel", map[string]string{"name": name, "tunnel_secret": secret, "config_src": "local"}, &t)
	return t, e
}

func (c Client) DeleteOwned(zone, name, kind, id string, dry bool) error {
	if !ident.MatchString(id) || !ident.MatchString(zone) {
		return fmt.Errorf("invalid ownership ledger")
	}
	records, e := c.Records(zone, name)
	if e != nil {
		return e
	}
	for _, r := range records {
		if r.ID != id {
			continue
		}
		if r.Name != name || r.Type != kind {
			return fmt.Errorf("owned record identity changed")
		}
		if dry {
			return nil
		}
		_, e = c.call("DELETE", "/zones/"+zone+"/dns_records/"+id, nil, nil)
		return e
	}
	return nil
}
