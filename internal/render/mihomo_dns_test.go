package render

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/routing"
	"gopkg.in/yaml.v3"
)

func TestMihomoResolverSeparation(t *testing.T) {
	s, _ := model.New("192.0.2.2")
	for _, policyName := range []string{"strict", "split"} {
		s.MihomoPolicy = policyName
		rules, _ := policy(*s)
		dns := mihomoDNS(*s, rules, false)
		for _, key := range []string{"default-nameserver", "nameserver", "proxy-server-nameserver", "direct-nameserver"} {
			v := dns[key].([]string)
			if len(v) != 3 {
				t.Fatal(key, v)
			}
			for _, n := range v {
				if strings.Contains(n, "alidns") || strings.Contains(n, "doh.pub") {
					t.Fatal(key, n)
				}
			}
		}
		m := dns["nameserver-policy"].(M)
		cnAI := m["+.moonshot.cn"].([]string)
		if policyName == "split" {
			if !strings.Contains(cnAI[0], "alidns") || !strings.HasSuffix(cnAI[0], "#DIRECT") {
				t.Fatal("domestic AI DNS split")
			}
		} else if !strings.HasSuffix(cnAI[0], "#AI") {
			t.Fatal("strict policy sent domestic AI to local DNS")
		}
		for _, h := range []string{"+.chatgpt.com", "+.oaistatic.com", "+.claude.ai"} {
			for _, r := range m[h].([]string) {
				if !strings.HasSuffix(r, "#AI") {
					t.Fatal(h, r)
				}
			}
		}
		_, has := m["www.12306.cn"]
		if has != (policyName == "split") {
			t.Fatal("wrong domestic policy")
		}
		if _, has = m["+.cn"]; has {
			t.Fatal("overbroad DNS exception")
		}
		if dns["fallback"] != nil || dns["ipv6"] != false {
			t.Fatal("unexpected fallback or IPv6")
		}
	}
	s.Rules.Rules = []routing.Rule{{Kind: "DOMAIN-KEYWORD", Value: "12306", Group: "AI"}}
	rules, _ := policy(*s)
	if _, ok := mihomoDNS(*s, rules, false)["nameserver-policy"].(M)["www.12306.cn"]; ok {
		t.Fatal("AI classification lost to domestic exception")
	}
	if Explain(*s, "tv.apple.com") != "Streaming (DOMAIN-SUFFIX tv.apple.com)" || Explain(*s, "captive.apple.com") != "DIRECT (DOMAIN captive.apple.com)" {
		t.Fatal("Apple service routing")
	}
}

type dohRecorder struct {
	*httptest.Server
	mu    sync.Mutex
	names []string
}

func newDoH(t *testing.T) *dohRecorder {
	t.Helper()
	r := new(dohRecorder)
	r.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var msg []byte
		var e error
		if req.Method == "GET" {
			msg, e = base64.RawURLEncoding.DecodeString(req.URL.Query().Get("dns"))
		} else {
			msg, e = io.ReadAll(io.LimitReader(req.Body, 4096))
		}
		if e != nil || len(msg) < 17 {
			w.WriteHeader(400)
			return
		}
		end := 12
		var labels []string
		for end < len(msg) && msg[end] > 0 {
			n := int(msg[end])
			end++
			if n > 63 || end+n >= len(msg) {
				w.WriteHeader(400)
				return
			}
			labels = append(labels, string(msg[end:end+n]))
			end += n
		}
		if end+5 > len(msg) {
			w.WriteHeader(400)
			return
		}
		r.mu.Lock()
		r.names = append(r.names, strings.Join(labels, "."))
		r.mu.Unlock()
		// A TXT answer forces upstream resolution even when Fake-IP is enabled.
		answer := append([]byte(nil), msg[:end+5]...)
		copy(answer[2:12], []byte{0x81, 0x80, 0, 1, 0, 1, 0, 0, 0, 0})
		answer = append(answer, 0xc0, 0x0c, 0, 16, 0, 1, 0, 0, 0, 1, 0, 3, 2, 'o', 'k')
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(answer)
	}))
	t.Cleanup(r.Close)
	return r
}
func (r *dohRecorder) saw(q string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.names, q)
}
func txtQuery(network, addr, name string) bool {
	c, e := net.DialTimeout(network, addr, time.Second)
	if e != nil {
		return false
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	q := []byte{0, 7, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, label := range strings.Split(name, ".") {
		q = append(q, byte(len(label)))
		q = append(q, label...)
	}
	q = append(q, 0, 0, 16, 0, 1)
	if network == "tcp4" {
		q = append([]byte{byte(len(q) >> 8), byte(len(q))}, q...)
	}
	if _, e = c.Write(q); e != nil {
		return false
	}
	reply := make([]byte, 2048)
	var n int
	if network == "tcp4" {
		var size [2]byte
		if _, e = io.ReadFull(c, size[:]); e != nil {
			return false
		}
		n = int(binary.BigEndian.Uint16(size[:]))
		if n > len(reply) {
			return false
		}
		_, e = io.ReadFull(c, reply[:n])
	} else {
		n, e = c.Read(reply)
	}
	return e == nil && n > 12 && reply[3]&15 == 0 && binary.BigEndian.Uint16(reply[6:8]) > 0 && bytes.HasSuffix(reply[:n], []byte{2, 'o', 'k'})
}
func freeAddr(t *testing.T) string {
	t.Helper()
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	return l.Addr().String()
}

// Run with MIHOMO_BIN set. Only resolver destinations, the network adapter,
// and local listener ports are replaced; domain policy and group detours are exported.
func TestMihomoLiveDNSPaths(t *testing.T) {
	bin := os.Getenv("MIHOMO_BIN")
	if bin == "" {
		t.Skip("requires MIHOMO_BIN")
	}
	foreign, domestic, bootstrap := newDoH(t), newDoH(t), newDoH(t)
	var offline atomic.Bool
	var forwarded atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || offline.Load() || r.Host != foreign.Listener.Addr().String() {
			w.WriteHeader(503)
			return
		}
		up, e := net.Dial("tcp", r.Host)
		if e != nil {
			w.WriteHeader(502)
			return
		}
		defer up.Close()
		client, rw, e := w.(http.Hijacker).Hijack()
		if e != nil {
			return
		}
		defer client.Close()
		forwarded.Add(1)
		fmt.Fprint(rw, "HTTP/1.1 200 OK\r\n\r\n")
		rw.Flush()
		go func() { _, _ = io.Copy(up, rw); up.Close() }()
		_, _ = io.Copy(client, up)
	}))
	defer proxy.Close()
	s, _ := model.New("127.0.0.1")
	s.MihomoPolicy = "split"
	testTun := os.Getenv("MIHOMO_TEST_TUN") == "1"
	s.Capture = "system"
	if testTun {
		s.Capture = "tun"
	}
	b, _ := s.Enable("ss")
	if e := s.Add("ss", "direct", s.Direct, b.Port); e != nil {
		t.Fatal(e)
	}
	data, _, e := Export(*s, "mihomo", "")
	if e != nil {
		t.Fatal(e)
	}
	var cfg M
	if e = yaml.Unmarshal(data, &cfg); e != nil {
		t.Fatal(e)
	}
	dn := cfg["dns"].(M)
	dest := func(raw string) string {
		u, group, _ := strings.Cut(raw, "#")
		if strings.HasPrefix(u, "rcode:") {
			return raw
		}
		server := foreign.URL
		if group == "" || group == "DIRECT" {
			server = bootstrap.URL
		}
		if strings.Contains(u, "alidns") || strings.Contains(u, "doh.pub") {
			server = domestic.URL
		}
		if strings.Contains(u, "1.1.1.1") && (group == "AI" || group == "Main") {
			server = "https://127.0.0.1:1"
		}
		if group != "" {
			group = "#" + group
		}
		return server + "/dns-query" + group
	}
	replace := func(v any) any {
		if xs, ok := v.([]any); ok {
			out := []string{}
			for _, x := range xs {
				out = append(out, dest(x.(string)))
			}
			return out
		}
		return dest(v.(string))
	}
	for _, key := range []string{"nameserver", "default-nameserver", "proxy-server-nameserver", "direct-nameserver"} {
		dn[key] = replace(dn[key])
	}
	for k, v := range dn["nameserver-policy"].(M) {
		dn["nameserver-policy"].(M)[k] = replace(v)
	}
	// Mihomo v1.19 uses this spelling for process-local additional TLS roots.
	cfg["tls"] = M{"custom-certifactes": []string{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: foreign.Certificate().Raw}))}}
	_, port, _ := net.SplitHostPort(proxy.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	cfg["proxies"] = []M{{"name": "direct-ss", "type": "http", "server": "127.0.0.1", "port": p}}
	for _, entry := range cfg["proxy-groups"].([]any) {
		g := entry.(M)
		if g["name"] == "Auto" {
			g["url"] = "http://127.0.0.1:1/test"
		}
	}
	start := func() (string, func()) {
		t.Helper()
		dir := t.TempDir()
		dnsAddr, mixed := freeAddr(t), freeAddr(t)
		_, p, _ := net.SplitHostPort(mixed)
		n, _ := strconv.Atoi(p)
		cfg["mixed-port"] = n
		dn["listen"] = dnsAddr
		cfg["dns"] = dn
		data, e := yaml.Marshal(cfg)
		if e != nil {
			t.Fatal(e)
		}
		f := filepath.Join(dir, "config.yaml")
		if e = os.WriteFile(f, data, 0600); e != nil {
			t.Fatal(e)
		}
		cmd := exec.CommandContext(t.Context(), bin, "-d", dir, "-f", f)
		log := new(bytes.Buffer)
		cmd.Stdout = log
		cmd.Stderr = log
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		stop := sync.OnceFunc(func() { cmd.Process.Kill(); cmd.Wait() })
		t.Cleanup(stop)
		for i := 0; i < 180; i++ {
			c, e := net.DialTimeout("tcp", mixed, 100*time.Millisecond)
			if e == nil {
				c.Close()
				if testTun {
					return "203.0.113.53:53", stop
				}
				return dnsAddr, stop
			}
			time.Sleep(250 * time.Millisecond)
		}
		stop()
		t.Fatal(log.String())
		return "", nil
	}
	addr, stop := start()
	for _, q := range []string{"api.openai.com", "assets.oaistatic.com", "api.anthropic.com", "unclassified-ai.example.cn", "www.12306.cn", "api.moonshot.cn", "api.deepseek.com"} {
		if !txtQuery("udp4", addr, q) {
			t.Fatalf("no TXT answer for %s", q)
		}
		if q == "www.12306.cn" || q == "api.moonshot.cn" || q == "api.deepseek.com" {
			if !domestic.saw(q) || foreign.saw(q) {
				t.Fatal("incorrect domestic upstream")
			}
		} else if !foreign.saw(q) || domestic.saw(q) || bootstrap.saw(q) {
			t.Fatal("incorrect upstream for", q)
		}
	}
	if !txtQuery("tcp4", addr, "tcp.claude.ai") || !foreign.saw("tcp.claude.ai") || domestic.saw("tcp.claude.ai") {
		t.Fatal("TCP DNS routing failed")
	}
	if forwarded.Load() == 0 {
		t.Fatal("foreign DoH did not traverse proxy")
	}
	stop()
	offline.Store(true)
	addr, _ = start()
	q := "outage.openai.com"
	if txtQuery("udp4", addr, q) || domestic.saw(q) || bootstrap.saw(q) || foreign.saw(q) {
		t.Fatal("DNS failed open after proxy outage")
	}
	if !txtQuery("udp4", addr, "api.deepseek.com") || !domestic.saw("api.deepseek.com") || !txtQuery("udp4", addr, "kyfw.12306.cn") || !domestic.saw("kyfw.12306.cn") {
		t.Fatal("domestic positive control failed during proxy outage")
	}
}
func TestMihomoFailoverAndCapture(t *testing.T) {
	s, _ := model.New("192.0.2.5")
	for _, id := range []string{"ss", "hysteria2", "reality", "trojan-ws", "vless-ws"} {
		a, e := s.Enable(id)
		if e != nil {
			t.Fatal(e)
		}
		mode, host := "direct", s.Direct
		if strings.HasSuffix(id, "-ws") {
			mode, host = "tunnel", "edge.example.org"
		}
		if e = s.Add(id, mode, host, a.Port); e != nil {
			t.Fatal(e)
		}
	}
	b, _, e := Export(*s, "mihomo", "")
	if e != nil {
		t.Fatal(e)
	}
	var cfg struct {
		IPv6 bool
		Tun  struct {
			Enable, Strict bool
			Hijack         []string `yaml:"dns-hijack"`
		}
		Groups []struct {
			Name, Type string
			Proxies    []string
		} `yaml:"proxy-groups"`
	}
	if e = yaml.Unmarshal(b, &cfg); e != nil {
		t.Fatal(e)
	}
	if cfg.IPv6 || !cfg.Tun.Enable || !slices.Contains(cfg.Tun.Hijack, "tcp://any:53") {
		t.Fatal("capture defaults")
	}
	if cfg.Groups[0].Proxies[0] != "Auto" || cfg.Groups[1].Type != "fallback" || !slices.Equal(cfg.Groups[1].Proxies, []string{"direct-reality", "direct-hysteria2", "tunnel-vless-ws", "tunnel-trojan-ws", "direct-ss"}) {
		t.Fatal("invalid fallback order")
	}
	s.Capture = "system"
	s.MihomoIPv6 = "on"
	b, _, _ = Export(*s, "mihomo", "")
	if bytes.Contains(b, []byte("tun:\n")) || !bytes.Contains(b, []byte("ipv6: true")) {
		t.Fatal("ignored capture or IPv6 override")
	}
}

// Each fixture represents a proxy node and terminates the loopback CONNECT tunnel.
// The /check response drives Mihomo's real health checker, /data identifies the selected node.
func TestMihomoLiveFallback(t *testing.T) {
	t.Run("auto", func(t *testing.T) { testFallback(t, false) })
	t.Run("preferred edge", func(t *testing.T) { testFallback(t, true) })
}

func testFallback(t *testing.T, preferred bool) {
	bin := os.Getenv("MIHOMO_BIN")
	if bin == "" {
		t.Skip("requires MIHOMO_BIN")
	}
	var down atomic.Bool
	node := func(id string, primary bool) M {
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "CONNECT" || (primary && down.Load()) {
				w.WriteHeader(503)
				return
			}
			c, b, e := w.(http.Hijacker).Hijack()
			if e != nil {
				return
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(3 * time.Second))
			fmt.Fprint(b, "HTTP/1.1 200 OK\r\n\r\n")
			b.Flush()
			req, e := http.ReadRequest(b.Reader)
			if e != nil {
				return
			}
			req.Body.Close()
			if req.URL.Path == "/check" {
				if preferred && !primary {
					time.Sleep(150 * time.Millisecond)
				}
				fmt.Fprint(b, "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
			} else {
				fmt.Fprintf(b, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(id), id)
			}
			b.Flush()
		}))
		t.Cleanup(proxy.Close)
		return M{"name": id, "type": "http", "server": "127.0.0.1", "port": proxy.Listener.Addr().(*net.TCPAddr).Port}
	}
	s, _ := model.New("127.0.0.1")
	s.Routing = "global"
	s.Capture = "system"
	ids := []string{"hysteria2", "reality"}
	mode, hostname, group := "direct", s.Direct, "Auto"
	first, last := "direct-reality", "direct-hysteria2"
	if preferred {
		ids = []string{"vless-ws"}
		mode, hostname, group = "tunnel", "edge.example.org", "Preferred Edge"
		s.PreferredEdge = model.EdgePreferences{Mode: "ips", Addresses: []string{"188.114.96.1"}}
		first, last = "tunnel-vless-ws / 188.114.96.1", "tunnel-vless-ws"
	}
	for _, id := range ids {
		a, e := s.Enable(id)
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Add(id, mode, hostname, a.Port); e != nil {
			t.Fatal(e)
		}
	}
	data, _, e := Export(*s, "mihomo", "")
	if e != nil {
		t.Fatal(e)
	}
	var cfg M
	if e = yaml.Unmarshal(data, &cfg); e != nil {
		t.Fatal(e)
	}
	cfg["proxies"] = []M{node(first, true), node(last, false)}
	if preferred {
		cfg["rules"] = []string{"MATCH,Preferred Edge"}
	}
	for _, item := range cfg["proxy-groups"].([]any) {
		g := item.(M)
		if g["name"] == group || (preferred && g["name"] == "Auto") {
			g["url"] = "http://192.0.2.1/check"
			g["interval"] = 1
		}
	}
	mixed := freeAddr(t)
	proxyURL, _ := url.Parse("http://" + mixed)
	p, _ := strconv.Atoi(proxyURL.Port())
	cfg["mixed-port"] = p
	cfg["dns"].(M)["listen"] = freeAddr(t)
	data, e = yaml.Marshal(cfg)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	if e = os.WriteFile(f, data, 0600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(t.Context(), bin, "-d", dir, "-f", f)
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	client := http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}}
	expect := func(id string) {
		t.Helper()
		until := time.Now().Add(12 * time.Second)
		for time.Now().Before(until) {
			resp, e := client.Get("http://192.0.2.1/data")
			if e == nil {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if string(body) == id {
					return
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("did not route through", id)
	}
	expect(first)
	down.Store(true)
	expect(last)
	down.Store(false)
	expect(first)
}

func TestSmartGeoRulesFollowServices(t *testing.T) {
	s, _ := model.New("192.0.2.1")
	s.MihomoPolicy = "split"
	s.Overrides = []routing.Rule{{Kind: "DOMAIN", Value: "fixture.example", Group: "DIRECT"}}
	for q, want := range map[string]string{"100.127.255.255": "DIRECT", "sub.msftncsi.com": "DIRECT", "cdn.doubleclick.net": "REJECT", "api.moonshot.cn": "AI-CN", "api.deepseek.com": "AI-CN", "generativelanguage.googleapis.com": "AI", "x.generativelanguage.googleapis.com": "Main", "img.ytimg.com": "Streaming", "telegram.me": "Communication", "media.whatsapp.net": "Communication", "discord.gg": "Communication", "raw.githubusercontent.com": "Main", "other.example": "Main"} {
		if got := Explain(*s, q); !strings.HasPrefix(got, want+" ") {
			t.Errorf("%s: %s", q, got)
		}
	}
	s.Overrides = []routing.Rule{{Kind: "DOMAIN", Value: "fixture.example", Group: "DIRECT"}}
	pre, end := policyBeforeGeo(*s)
	rules := mihomoRules(*s, pre, end)
	want := []string{"GEOSITE,CN,DIRECT", "GEOIP,CN,DIRECT,no-resolve", "MATCH,Main"}
	if !slices.Equal(rules[len(rules)-3:], want) || !slices.Contains(rules, "DOMAIN,fixture.example,DIRECT") {
		t.Fatal("geo/final precedence")
	}
	if strings.Contains(strings.Join(rules, "\n"), "RULE-SET,cn-domains") || strings.Contains(strings.Join(rules, "\n"), "RULE-SET,cn-ips") || len(rules) > 100 {
		t.Fatal("expanded geographic payload")
	}
	dns := mihomoDNS(*s, pre, false)
	if _, ok := dns["nameserver-policy"].(M)["fixture.example"]; ok {
		t.Fatal("geosite leaked into DNS policy")
	}
	inbound, err := s.Enable("ss")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Add("ss", "direct", s.Direct, inbound.Port); err != nil {
		t.Fatal(err)
	}
	config, _, err := Export(*s, "sing-box", "")
	if err != nil {
		t.Fatal(err)
	}
	var native M
	if err = json.Unmarshal(config, &native); err != nil {
		t.Fatal(err)
	}
	sets := native["route"].(map[string]any)["rule_set"].([]any)
	if len(sets) != 2 || native["experimental"].(map[string]any)["cache_file"].(map[string]any)["enabled"] != true {
		t.Fatal("native geographic rules are not remotely cached")
	}
}

func TestNodeDNSOnlyForExportedHostnames(t *testing.T) {
	s, _ := model.New("fast.example.org")
	for _, id := range []string{"ss", "reality", "hysteria2"} {
		a, e := s.Enable(id)
		if e != nil {
			t.Fatal(e)
		}
		host := map[string]string{"ss": "fast.example.org", "reality": "edge.example.org", "hysteria2": "192.0.2.1"}[id]
		if e = s.Add(id, "direct", host, a.Port); e != nil {
			t.Fatal(e)
		}
	}
	for _, c := range []struct {
		setting, filter string
		count           int
	}{{"", "", 0}, {"alidns", "", 2}, {"alidns", "direct-ss", 1}, {"alidns", "direct-hysteria2", 0}} {
		s.MihomoNodeDNS = c.setting
		data, _, e := Export(*s, "mihomo", c.filter)
		if e != nil {
			t.Fatal(e)
		}
		var cfg struct {
			DNS struct {
				Policy    map[string][]string `yaml:"proxy-server-nameserver-policy"`
				Main      []string            `yaml:"nameserver"`
				Bootstrap []string            `yaml:"proxy-server-nameserver"`
			}
		}
		if e = yaml.Unmarshal(data, &cfg); e != nil {
			t.Fatal(e)
		}
		if len(cfg.DNS.Policy) != c.count {
			t.Fatal(c)
		}
		for k, v := range cfg.DNS.Policy {
			if (k != "fast.example.org" && k != "edge.example.org") || !slices.Equal(v, []string{"https://223.5.5.5/dns-query#DIRECT", "https://223.6.6.6/dns-query#DIRECT"}) {
				t.Fatal(k, v)
			}
		}
		for _, u := range append(cfg.DNS.Main, cfg.DNS.Bootstrap...) {
			if strings.Contains(u, "223.") {
				t.Fatal("resolver escaped node policy")
			}
		}
	}
}
