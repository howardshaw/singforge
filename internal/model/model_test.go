package model

import "testing"

func TestEndpointChangesPreserveAuth(t *testing.T) {
	s, _ := New("192.0.2.2")
	a, e := s.Enable("reality")
	if e != nil {
		t.Fatal(e)
	}
	s.Add("reality", "direct", s.Direct, 443)
	before := *a
	s.Add("reality", "direct", "2001:db8::1", 444)
	if e = s.Validate(); e != nil || s.Inbounds[0] != before || s.Nodes()[0].Host != "2001:db8::1" {
		t.Fatal("endpoint mutation", e)
	}
	if e = s.Add("reality", "cdn", "example.org", 443); e == nil {
		t.Fatal("raw TCP through CDN")
	}
	if Host("x;systemctl reboot") || Host("../escape") || Host("a\nb") {
		t.Fatal("malformed host")
	}
	s.Inbounds[0].Public = "wrong"
	if s.Validate() == nil {
		t.Fatal("mismatched keys")
	}
}

func TestSharedPortAndHandshake(t *testing.T) {
	s, _ := New("192.0.2.1")
	a, _ := s.Enable("reality")
	a.Port = 443
	b, _ := s.Enable("hysteria2")
	b.Port = 443
	s.RealitySNI = "site.example.org"
	s.RealityTarget = "127.0.0.1:9443"
	if e := s.Validate(); e != nil {
		t.Fatal(e)
	}
	s.RealityTarget = "127.0.0.1:443"
	if s.Validate() == nil {
		t.Fatal("accepted handshake loop")
	}
	s.RealityTarget = "[::1]:9443"
	c, _ := s.Enable("tuic")
	c.Port = 443
	if s.Validate() == nil {
		t.Fatal("accepted conflicting UDP listeners")
	}
}
