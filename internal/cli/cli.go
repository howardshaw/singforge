package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"

	"github.com/howardshaw/singforge/internal/host"
	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/render"
	"github.com/howardshaw/singforge/internal/store"
)

type opts struct {
	quiet                           bool
	all                             bool
	str                             map[string]*string
	yes, dry, qr, purge, full, json bool
	port                            int
	ttl, grace                      time.Duration
}

func (o opts) get(k string) string {
	if v := o.str[k]; v != nil {
		return *v
	}
	return ""
}
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
func ids() []string {
	var v []string
	for _, s := range model.Catalog {
		v = append(v, s.ID)
	}
	return v
}
func Direct(s *model.State, id string, port int) error {
	sp, ok := model.Lookup(id)
	if !ok || sp.WS {
		return fmt.Errorf("select a direct protocol")
	}
	a, e := s.Enable(id)
	if e != nil {
		return e
	}
	if port != 0 {
		a.Port = port
	}
	for i := range s.Endpoints {
		ep := &s.Endpoints[i]
		if ep.ID == "direct-"+id {
			if port != 0 {
				ep.Port = port
			}
			return nil
		}
	}
	return s.Add(id, "direct", s.Direct, a.Port)
}
func read(path string) (string, error) { b, e := os.ReadFile(path); return string(b), e }
func Edge(s *model.State, mode, h string, o opts) error {
	if !model.Domain(h) {
		return fmt.Errorf("edge requires --host with a DNS name")
	}
	p := 443
	switch mode {
	case "cdn":
		s.CDN = h
		p = o.port
		if p == 0 {
			p = 2053
		}
		s.CDNPort = p
		var e error
		s.OriginCert, e = read(o.get("cert"))
		if e != nil {
			return e
		}
		s.OriginKey, e = read(o.get("key"))
		if e != nil {
			return e
		}
	case "tunnel":
		s.Tunnel = h
		if o.get("credentials") != "" {
			c, e := read(o.get("credentials"))
			if e != nil {
				return e
			}
			s.TunnelCredentials = c
			s.ConnectorToken = ""
		}
		if p := o.get("connector-token-file"); p != "" {
			if o.get("credentials") != "" {
				return fmt.Errorf("select one tunnel credential type")
			}
			token, e := read(p)
			if e != nil {
				return e
			}
			s.ConnectorToken = strings.TrimSpace(token)
			s.TunnelCredentials = ""
		}
		id, e := host.TunnelID(s.TunnelCredentials)
		if s.ConnectorToken != "" {
			id, e = host.ConnectorID(s.ConnectorToken)
		}
		if e != nil {
			return e
		}
		s.TunnelID = id
	default:
		return fmt.Errorf("expected cdn or tunnel")
	}
	ids := []string{"vless-ws", "trojan-ws"}
	if o.get("preset") == "full" {
		ids = append(ids, "vmess-ws")
	}
	for _, id := range ids {
		if _, e := s.Enable(id); e != nil {
			return e
		}
		if e := s.Add(id, mode, h, p); e != nil {
			return e
		}
	}
	return nil
}
func install(s *model.State, o opts, exists bool) error {
	if exists {
		return fmt.Errorf("already installed; manage endpoints and protocols through their commands")
	}
	h := o.get("host")
	mode := o.get("mode")
	edgeOnly := mode == "tunnel" || mode == "cdn"
	if edgeOnly && h == "" {
		h = "127.0.0.1"
	}
	if h == "" {
		var e error
		h, e = host.DetectAddress()
		if e != nil {
			return e
		}
	}
	n, e := model.New(h)
	if e != nil {
		return e
	}
	*s = *n
	s.Listen = o.get("listen")
	s.Handshake = o.get("handshake")
	s.RealityTarget = o.get("handshake-target")
	s.ServerDNSFamily = o.get("server-family")
	if edgeOnly {
		s.Direct = "127.0.0.1"
		edge := o.get(mode)
		if edge == "" {
			edge = o.get("host")
		}
		return Edge(s, mode, edge, o)
	}
	list := []string{"reality", "hysteria2", "ss"}
	switch o.get("preset") {
	case "minimal":
		list = []string{"ss"}
	case "recommended":
	case "custom":
		list = strings.Split(o.get("protocols"), ",")
	case "full":
		list = nil
		for _, sp := range model.Catalog {
			if !sp.WS {
				list = append(list, sp.ID)
			}
		}
	default:
		return fmt.Errorf("unknown preset")
	}
	for _, id := range list {
		if e = Direct(s, id, 0); e != nil {
			return e
		}
	}
	switch o.get("mode") {
	case "direct":
		return nil
	case "hybrid":
		if e = Edge(s, "cdn", o.get("cdn"), o); e != nil {
			return e
		}
		return Edge(s, "tunnel", o.get("tunnel"), o)
	default:
		return fmt.Errorf("expected direct or hybrid mode")
	}
}
func execute(cmd string, o opts, handler func(operation) error) error {
	if cmd == "protocol list" && o.get("workspace") == "" {
		return printJSON(model.Catalog)
	}
	var e error
	m := host.Manager{Dir: o.get("workspace"), Core: o.get("core")}
	if m.Dir == "" {
		if e = host.Preflight(); e != nil {
			return e
		}
		os.Setenv("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
		m.Live = true
		m.Dir = store.Root
	} else if !filepath.IsAbs(m.Dir) || filepath.Clean(m.Dir) == store.Root {
		return fmt.Errorf("use a separate absolute workspace directory")
	}
	if cmd == "uninstall" && o.full {
		if !o.dry && !o.yes {
			return fmt.Errorf("full uninstall requires --yes")
		}
		if o.dry {
			return fullUninstall(m, true)
		}
		if !m.Live {
			if _, err := m.RemovalPlan(); err != nil {
				return err
			}
		}
		unlock, err := store.Lock(m.Dir)
		if err != nil {
			return err
		}
		defer unlock()
		return fullUninstall(m, false)
	}
	var wait time.Duration
	if cmd == "certificate auto-renew sync" && os.Getenv("RENEWED_LINEAGE") != "" {
		wait = 2 * time.Minute
	}
	unlock, e := store.LockFor(m.Dir, wait)
	if e != nil {
		return e
	}
	defer unlock()
	if o.dry {
		if _, err := os.Lstat(filepath.Join(m.Dir, "transaction")); err == nil {
			return fmt.Errorf("pending transaction; run doctor to recover before preview")
		}
	}
	if e = m.Recover(); e != nil {
		return e
	}
	s, e := store.Read(m.Dir)
	exists := e == nil
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if !exists && cmd != "install" && cmd != "provision" && cmd != "backup restore" && cmd != "protocol list" {
		return fmt.Errorf("no installation in %s", m.Dir)
	}
	return handler(operation{cmd, o, m, s, exists})
}

type operation struct {
	path      string
	options   opts
	manager   host.Manager
	state     model.State
	installed bool
}

func (op operation) commit(s model.State, secret string) error {
	o, m := op.options, op.manager
	var e error

	if o.dry {
		if _, e := host.Files(s, "sing-box", "cloudflared"); e != nil {
			return e
		}
		return printJSON(s.Endpoints)
	}
	if e = m.Apply(s); e != nil {
		return e
	}
	if o.json {
		return printJSON(map[string]any{"applied": true, "issued_url": secret})
	}
	if secret != "" {
		fmt.Println(secret)
	} else {
		fmt.Println("applied")
	}
	return nil
}
func export(s model.State, o opts) error {
	b, w, e := render.Export(s, o.get("format"), o.get("filter"))
	for _, v := range w {
		fmt.Fprintln(os.Stderr, v)
	}
	if e != nil {
		return e
	}
	if o.qr {
		q, e := qrcode.New(strings.TrimSpace(string(b)), qrcode.Medium)
		if e != nil {
			return fmt.Errorf("export exceeds QR capacity; select one URI using --format uri --filter ID")
		}
		if o.get("output") == "" {
			fmt.Print(q.ToSmallString(false))
			return nil
		}
		b, e = q.PNG(512)
		if e != nil {
			return e
		}
	}
	if p := o.get("output"); p != "" {
		f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = f.Write(b)
		return errors.Join(e, f.Close())
	}
	_, e = os.Stdout.Write(b)
	return e
}
