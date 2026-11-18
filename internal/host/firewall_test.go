package host

import (
	"bytes"
	"strings"
	"testing"
)

func TestPersistentRulesKeepSSH(t *testing.T) {
	base := []byte("*filter\n:INPUT DROP [0:0]\n-A INPUT -p tcp --dport 22 -j ACCEPT\n-A INPUT -j REJECT --reject-with icmp-host-prohibited\nCOMMIT\n*nat\nCOMMIT\n")
	p := []Port{{"tcp", 8881}, {"udp", 8882}}
	b, e := persistentRules(base, nil, p)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Index(string(b), "--dport 22") > strings.Index(string(b), "--dport 8881") || strings.Index(string(b), "--dport 8882") > strings.Index(string(b), "-j REJECT") {
		t.Fatal("wrong rule priority")
	}
	same, e := persistentRules(b, p, p)
	if e != nil || !bytes.Equal(same, b) {
		t.Fatal("repeated sync changed config")
	}
	clean, e := persistentRules(b, p, nil)
	if e != nil || !bytes.Equal(clean, base) {
		t.Fatal("unrelated firewall changes")
	}
}
