package subscription

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/render"
)

type Bundle struct {
	Host     string
	Grants   []model.Grant
	Exports  map[string][]byte
	Warnings map[string]int
}

func Build(s model.State) Bundle {
	b := Bundle{s.PublishHost, s.Grants, map[string][]byte{}, map[string]int{}}
	for _, f := range []string{"mihomo", "provider", "sing-box", "uri", "shadowsocks", "v2ray"} {
		d, w, e := render.Export(s, f, "")
		if e == nil {
			b.Exports[f] = d
			b.Warnings[f] = len(w)
		}
	}
	return b
}
func Rotate(s *model.State, now time.Time, ttl, grace time.Duration) string {
	var grants []model.Grant
	if grace > 0 {
		end := now.Add(grace)
		for _, g := range s.Grants {
			if !g.Expires.IsZero() && !g.Expires.After(now) {
				continue
			}
			if g.Expires.IsZero() || g.Expires.After(end) {
				g.Expires = end
			}
			grants = append(grants, g)
		}
	}
	s.Grants = grants
	return Issue(s, now, ttl)
}

// Issue adds a grant without shortening any existing grant.
func Issue(s *model.State, now time.Time, ttl time.Duration) string {
	s.Grants = slices.DeleteFunc(s.Grants, func(g model.Grant) bool { return !g.Expires.IsZero() && !g.Expires.After(now) })
	token := base64.RawURLEncoding.EncodeToString(model.Random(32))
	sum := sha256.Sum256([]byte(token))
	g := model.Grant{Digest: hex.EncodeToString(sum[:])}
	if ttl > 0 {
		g.Expires = now.Add(ttl)
	}
	s.Grants = append(s.Grants, g)
	return token
}
func Handler(b Bundle) http.Handler {
	var mu sync.Mutex
	var start time.Time
	count := 0
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		now := time.Now()
		mu.Lock()
		if now.Sub(start) >= time.Minute {
			start = now
			count = 0
		}
		count++
		limited := count > 120
		mu.Unlock()
		if limited {
			w.WriteHeader(429)
			return
		}
		parts := strings.Split(r.URL.Path, "/")
		if r.Method != "GET" || r.Host != b.Host || r.ContentLength != 0 || len(r.TransferEncoding) > 0 || r.URL.RawQuery != "" || r.URL.RawPath != "" || len(parts) != 4 || parts[1] != "s" || len(parts[2]) != 43 {
			w.WriteHeader(404)
			return
		}
		digest := sha256.Sum256([]byte(parts[2]))
		valid := 0
		for _, g := range b.Grants {
			d, e := hex.DecodeString(g.Digest)
			if e == nil && (g.Expires.IsZero() || now.Before(g.Expires)) {
				valid |= subtle.ConstantTimeCompare(d, digest[:])
			}
		}
		data, ok := b.Exports[parts[3]]
		if valid != 1 || !ok {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if b.Warnings[parts[3]] > 0 {
			w.Header().Set("X-SingForge-Omitted", fmt.Sprint(b.Warnings[parts[3]]))
		}
		_, _ = w.Write(data)
	})
}
func Serve(path string) error {
	var d []byte
	var e error
	if path == "-" {
		d, e = io.ReadAll(io.LimitReader(os.Stdin, 64<<20))
	} else {
		d, e = os.ReadFile(path)
	}
	if e != nil {
		return e
	}
	var b Bundle
	if e = json.Unmarshal(d, &b); e != nil {
		return e
	}
	if !model.Domain(b.Host) {
		return fmt.Errorf("invalid publication hostname")
	}
	srv := http.Server{Addr: "127.0.0.1:19781", Handler: Handler(b), MaxHeaderBytes: 4096, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 15 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
	return srv.ListenAndServe()
}
