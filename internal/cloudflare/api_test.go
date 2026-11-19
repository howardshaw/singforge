package cloudflare

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReconcile(t *testing.T) {
	zone := Zone{ID: strings.Repeat("a", 32), Name: "example.org"}
	record := Record{Type: "A", Name: "fast.example.org", Content: "192.0.2.1"}
	id := strings.Repeat("b", 32)
	var current []Record
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer scoped" {
			t.Error("missing bearer")
		}
		var result any = current
		if r.Method != "GET" {
			writes++
			var v Record
			if r.Method == "PATCH" {
				v = current[0]
			}
			if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
				t.Error(e)
			}
			v.ID = id
			current = []Record{v}
			result = v
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	defer srv.Close()
	c := Client{srv.URL, "scoped", srv.Client()}
	owned, e := c.Sync(zone, record, "", "", true)
	if owned != "" || e != nil || writes != 0 {
		t.Fatal("dry run")
	}
	owned, e = c.Sync(zone, record, "", "", false)
	if owned != id || e != nil {
		t.Fatal("create", e)
	}
	if _, e = c.Sync(zone, record, "", "", false); e != nil || writes != 1 {
		t.Fatal("idempotence")
	}
	record.Content = "192.0.2.2"
	if _, e = c.Sync(zone, record, "", "", false); e == nil || writes != 1 {
		t.Fatal("unowned update")
	}
	if _, e = c.Sync(zone, record, owned, "", false); e != nil || writes != 2 {
		t.Fatal("owned update")
	}
	record.Proxied = true
	owned, e = c.Sync(zone, record, "", id, false)
	if e != nil || owned != "" {
		t.Fatal("confirmed update must preserve external ownership")
	}
	current = append(current, current[0])
	if _, e = c.Sync(zone, record, id, "", false); e == nil {
		t.Fatal("ambiguous records")
	}
}
func TestTunnelAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			fmt.Fprint(w, `{"success":true,"result":{"id":"tunnel","name":"edge"}}`)
		} else if strings.HasSuffix(r.URL.Path, "/cfd_tunnel") {
			fmt.Fprint(w, `{"success":true,"result":[]}`)
		} else {
			http.Error(w, "sensitive response", http.StatusForbidden)
		}
	}))
	defer srv.Close()
	c := Client{srv.URL, "scoped", srv.Client()}
	v, e := c.Create(strings.Repeat("a", 32), "edge", "secret")
	if e != nil || v.Name != "edge" {
		t.Fatal(e)
	}
	_, e = c.Zone("example.org")
	if e == nil || strings.Contains(e.Error(), "sensitive") {
		t.Fatal("error redaction")
	}
}

func TestDNSWriteConflictIsReadBack(t *testing.T) {
	z := Zone{ID: strings.Repeat("a", 32), Name: "example.org"}
	want := Record{Type: "A", Name: "sub.example.org", Content: "192.0.2.2"}
	id := strings.Repeat("b", 32)
	for _, method := range []string{"POST", "PATCH"} {
		for _, converged := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/converged=%t", method, converged), func(t *testing.T) {
				reads, writes := 0, 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "GET" {
						reads++
						var records []Record
						if method == "PATCH" || writes > 0 {
							record := want
							record.ID = id
							if writes == 0 || !converged {
								record.Content = "192.0.2.1"
							}
							records = []Record{record}
						}
						json.NewEncoder(w).Encode(map[string]any{"success": true, "result": records})
						return
					}
					if r.Method != method {
						t.Errorf("unexpected %s", r.Method)
					}
					if method == "PATCH" {
						var fields map[string]any
						if err := json.NewDecoder(r.Body).Decode(&fields); err != nil || len(fields) != 1 || fields["content"] != want.Content {
							t.Errorf("unexpected patch: %v", fields)
						}
					}
					writes++
					http.Error(w, "private upstream error", http.StatusConflict)
				}))
				defer srv.Close()
				c := Client{srv.URL, "token", srv.Client()}
				owned := ""
				if method == "PATCH" {
					owned = id
				}
				got, err := c.Sync(z, want, owned, "", false)
				if (err == nil) != converged || reads != 2 || writes != 1 || (converged && got != owned) {
					t.Fatalf("got %q, %v; reads=%d writes=%d", got, err, reads, writes)
				}
			})
		}
	}
}

func TestTunnelCreateRaceDoesNotAdoptOtherCredentials(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		switch {
		case r.Method == "POST":
			http.Error(w, "untrusted response", http.StatusConflict)
		case len(methods) == 1:
			fmt.Fprint(w, `{"success":true,"result":[]}`)
		default:
			fmt.Fprint(w, `{"success":true,"result":[{"name":"edge","id":"other-id"}]}`)
		}
	}))
	defer srv.Close()
	c := Client{srv.URL, "token", srv.Client()}
	tunnel, err := c.Create(strings.Repeat("a", 32), "edge", "secret")
	if tunnel.ID != "" || err == nil || !strings.Contains(err.Error(), "other-id") || strings.Join(methods, ",") != "GET,POST,GET" {
		t.Fatal(tunnel, err, methods)
	}
}
