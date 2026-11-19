package model

import "testing"

func TestEdgePreferenceValidation(t *testing.T) {
	for _, p := range []EdgePreferences{
		{"ips", []string{"127.0.0.1"}}, {"ips", []string{"104.16.0.0/13"}},
		{"ips", []string{"2606:4700::1%en0"}}, {"ips", []string{"edge.example.org"}},
		{"domains", []string{"104.16.1.1"}}, {"default", []string{"edge.example.org"}},
		{"ips", []string{"188.114.96.1", "188.114.96.1"}},
	} {
		if err := p.Validate(); err == nil {
			t.Fatal("accepted invalid edge setting", p)
		}
	}
	if err := (EdgePreferences{Mode: "ips", Addresses: []string{"188.114.96.1", "2606:4700::1"}}).Validate(); err != nil {
		t.Fatal(err)
	}
}
