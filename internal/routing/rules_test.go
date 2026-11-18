package routing

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestPinnedClassicalRules(t *testing.T) {
	b := []byte("payload:\n - DOMAIN-SUFFIX,example.org\n - IP-CIDR,192.0.2.0/24,no-resolve\n - PROCESS-NAME,example\n")
	s := Source{"test", "AI", fmt.Sprintf("%x", sha256.Sum256(b)), "classical"}
	r, e := Load(s, b)
	if e != nil || len(r) != 2 || !r[0].Match("a.example.org") || r[0].Match("fakeexample.org") || !r[1].Match("192.0.2.1") {
		t.Fatal(r, e)
	}
	if _, e = Load(s, append(b, ' ')); e == nil {
		t.Fatal("accepted altered data")
	}
	if e = (Rule{"DOMAIN-SUFFIX", "bad,REJECT", "AI"}).Check(); e == nil {
		t.Fatal("accepted rule injection")
	}
}
