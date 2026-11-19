package cli

import (
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestProvisionEnv(t *testing.T) {
	core := "PUBLIC_IP=192.0.2.2\n"
	for _, v := range []string{"ENABLE_TUNNEL=yes", "DOMAIN=$(whoami)", "PUBLIC_IP=1.1.1.1", "PREFIX=foo\nDOMAIN=example.org\nCF_API_TOKEN_FILE=relative", "ENABLE_TUNNEL=true", "REALITY_PORT=8892", "UNKNOWN=secret"} {
		if _, e := parseProvision(strings.NewReader(core + v)); e == nil {
			t.Fatal("accepted invalid setting")
		}
	}
	c, e := parseProvision(strings.NewReader(core + "DOMAIN=example.org\nPREFIX=node2\nCF_API_TOKEN_FILE=/root/token\nENABLE_TUNNEL=true"))
	if e != nil || c.hostname("edge") != "node2-edge.example.org" || c.SS != 8892 {
		t.Fatal(e)
	}
}

func TestProvisionProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var log strings.Builder
		report, stop := provisionProgress(&log, 10*time.Second)
		report("Downloading sing-box")
		time.Sleep(11 * time.Second)
		report("Checking service ports")
		stop()
		const want = "[provision] Downloading sing-box\n[provision] Downloading sing-box (10s elapsed)\n[provision] Checking service ports\n"
		if got := log.String(); got != want {
			t.Fatalf("progress = %q, want %q", got, want)
		}
	})
}
