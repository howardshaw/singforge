package cli

import (
	"testing"

	"github.com/howardshaw/singforge/internal/model"
)

func TestIssuedCertbotName(t *testing.T) {
	host := "sub.example.org"
	pinned := host + "-0001"
	for _, tc := range []struct{ previous, output, want string }{
		{"", "Certificate is saved at: /etc/letsencrypt/live/sub.example.org-0001/fullchain.pem\nKey is saved at: /etc/letsencrypt/live/sub.example.org-0001/privkey.pem", pinned},
		{pinned, "Certificate not yet due for renewal", pinned},
		{"", "Certificate not yet due for renewal", host},
		{"", "Certificate is saved at: /etc/letsencrypt/live/other.example.org/fullchain.pem", ""},
		{"", "Certificate is saved at: /tmp/sub.example.org-0001/fullchain.pem", ""},
	} {
		s := model.State{PublishHost: host, CertbotName: tc.previous}
		got, err := issuedCertbotName(s, []byte(tc.output))
		if got != tc.want || (err != nil) != (tc.want == "") {
			t.Errorf("got %q, %v for %q", got, err, tc.output)
		}
	}
}
