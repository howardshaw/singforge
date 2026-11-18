package subscription

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/howardshaw/singforge/internal/model"
)

func TestGrantLifecycleAndRequestBoundary(t *testing.T) {
	s := model.State{PublishHost: "sub.example.org"}
	now := time.Now()
	first := Rotate(&s, now, time.Hour, 0)
	second := Rotate(&s, now, time.Hour, time.Minute)
	if len(first) != 43 || first == second || s.Grants[0].Digest == first || s.Grants[0].Expires.Sub(now) != time.Minute {
		t.Fatal("token rotation")
	}
	check := func(token, host, path string, want int) {
		t.Helper()
		b := Build(s)
		b.Exports["uri"] = []byte("secret")
		r := httptest.NewRequest("GET", "https://"+host+strings.ReplaceAll(path, "TOKEN", token), nil)
		w := httptest.NewRecorder()
		Handler(b).ServeHTTP(w, r)
		if w.Code != want || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") || want != 200 && strings.Contains(w.Body.String(), "secret") {
			t.Fatalf("status %d, expected %d", w.Code, want)
		}
	}
	for _, tok := range []string{first, second} {
		check(tok, s.PublishHost, "/s/TOKEN/uri", 200)
	}
	for _, path := range []string{"/sub.txt", "/s/TOKEN/../uri", "/s/TOKEN/%75ri", "/s/TOKEN/uri?x=1", "/s/TOKEN/unknown"} {
		check(second, s.PublishHost, path, 404)
	}
	check(second, "evil.example", "/s/TOKEN/uri", 404)
	s.Grants[0].Expires = now.Add(-time.Second)
	check(first, s.PublishHost, "/s/TOKEN/uri", 404)
	s.Grants = nil
	check(second, s.PublishHost, "/s/TOKEN/uri", 404)
}
func TestLimit(t *testing.T) {
	h := Handler(Bundle{})
	for i := 0; i < 121; i++ {
		r := httptest.NewRequest("GET", "http://localhost/sub.txt", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if i == 120 && w.Code != 429 {
			t.Fatal(w.Code)
		}
	}
}
