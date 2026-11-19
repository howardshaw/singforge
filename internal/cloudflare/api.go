// Package cloudflare reconciles individual records against an explicitly selected zone.
package cloudflare

import (
	"bytes"
	"encoding/json"
	"errors"
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

var errConflict = errors.New("resource conflict at Cloudflare (HTTP 409)")

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
	if res.StatusCode == http.StatusConflict {
		return 0, errConflict
	}
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
		// During preflight a new tunnel has no UUID yet.
		if !(dry && w.Content == "") && !model.Domain(w.Content) {
			return "", fmt.Errorf("invalid CNAME")
		}
	default:
		return "", fmt.Errorf("unsupported DNS type")
	}
	record, e := c.matchRecord(z, w)
	if e != nil {
		return "", e
	}
	var created Record
	base := "/zones/" + z.ID + "/dns_records"
	switch {
	case record != nil:
		if sameRecord(*record, w) {
			if record.ID == owned {
				return owned, nil
			}
			return "", nil
		}
		if record.ID != owned && record.ID != replace {
			return "", fmt.Errorf("conflict with existing %s %s (%s, proxied=%t); confirm using --replace-id %s", record.Type, record.Name, record.Content, record.Proxied, record.ID)
		}
		if dry {
			return owned, nil
		}
		patch := map[string]any{}
		if !sameContent(*record, w) {
			patch["content"] = w.Content
		}
		if record.Proxied != w.Proxied {
			patch["proxied"] = w.Proxied
		}
		_, e = c.call("PATCH", base+"/"+record.ID, patch, nil)
		if e == nil {
			return owned, nil
		}
	case dry:
		return "", nil
	default:
		_, e = c.call("POST", base, w, &created)
		if e == nil {
			if !ident.MatchString(created.ID) {
				return "", fmt.Errorf("invalid created record ID")
			}
			return created.ID, nil
		}
	}
	if !errors.Is(e, errConflict) {
		return "", e
	}
	// Another writer may have completed the same change. Read once, without
	// acquiring ownership of a record created by that writer.
	current, err := c.matchRecord(z, w)
	if err != nil {
		return "", err
	}
	if current != nil && sameRecord(*current, w) {
		if current.ID == owned {
			return owned, nil
		}
		return "", nil
	}
	return "", fmt.Errorf("%s %s still differs after refresh: %w", w.Type, w.Name, e)
}

func sameContent(a, w Record) bool {
	if w.Type == "CNAME" {
		return strings.EqualFold(strings.TrimSuffix(a.Content, "."), strings.TrimSuffix(w.Content, "."))
	}
	ip := net.ParseIP(a.Content)
	return ip != nil && ip.Equal(net.ParseIP(w.Content))
}
func sameRecord(a, w Record) bool { return sameContent(a, w) && a.Proxied == w.Proxied }

func (c Client) matchRecord(z Zone, w Record) (*Record, error) {
	records, e := c.Records(z.ID, w.Name)
	if e != nil {
		return nil, e
	}
	var candidates []Record
	for _, a := range records {
		if a.Name != w.Name {
			continue
		}
		if a.Type != w.Type {
			if a.Type == "NS" || a.Type == "CNAME" || w.Type == "CNAME" {
				return nil, fmt.Errorf("%s has incompatible %s record %s", w.Name, a.Type, a.ID)
			}
			continue
		}
		candidates = append(candidates, a)
	}
	if len(candidates) > 1 {
		return nil, fmt.Errorf("ambiguous %s records for %s; resolve explicitly in Cloudflare", w.Type, w.Name)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	if !ident.MatchString(candidates[0].ID) {
		return nil, fmt.Errorf("invalid record ID")
	}
	return &candidates[0], nil
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

// CheckTunnel accepts an existing tunnel only with the ID from local credentials.
func (c Client) CheckTunnel(account, name, credentialID string) (string, error) {
	if !model.Host(name) {
		return "", fmt.Errorf("invalid tunnel name")
	}
	tunnels, err := c.Tunnels(account)
	if err != nil {
		return "", err
	}
	foundID := false
	for _, t := range tunnels {
		if strings.EqualFold(t.Name, name) && (credentialID == "" || t.ID != credentialID) {
			return t.ID, fmt.Errorf("tunnel %s already exists (ID %s); restore matching tunnel credentials before provisioning, or choose a unique prefix for a new host", name, t.ID)
		}
		if t.ID == credentialID && credentialID != "" {
			foundID = true
		}
	}
	if credentialID != "" && !foundID {
		return "", fmt.Errorf("configured tunnel %s is missing in the selected account", credentialID)
	}
	return credentialID, nil
}

func (c Client) Create(account, name, secret string) (Tunnel, error) {
	var t Tunnel
	if !ident.MatchString(account) || !model.Host(name) {
		return t, fmt.Errorf("invalid tunnel account or name")
	}
	if _, err := c.CheckTunnel(account, name, ""); err != nil {
		return t, err
	}
	_, e := c.call("POST", "/accounts/"+account+"/cfd_tunnel", map[string]string{"name": name, "tunnel_secret": secret, "config_src": "local"}, &t)
	if errors.Is(e, errConflict) {
		if _, err := c.CheckTunnel(account, name, ""); err != nil {
			return t, err
		}
		return t, fmt.Errorf("create tunnel %s: %w", name, e)
	}
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
		if errors.Is(e, errConflict) {
			fresh, err := c.Records(zone, name)
			if err != nil {
				return err
			}
			for _, rr := range fresh {
				if rr.ID == id {
					return fmt.Errorf("delete %s %s: %w", kind, name, e)
				}
			}
			return nil
		}
		return e
	}
	return nil
}
