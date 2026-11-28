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
		for _, key := range []string{"nameserver", "proxy-server-nameserver", "direct-nameserver"} {
			v := dns[key].([]string)
			if len(v) != 3 {
				t.Fatal(key, v)
			}
			for _, n := range v {
				if strings.Contains(n, "223.5.5.5") || strings.Contains(n, "223.6.6.6") || strings.Contains(n, "doh.pub") {
					t.Fatal(key, n)
				}
			}
		}
		for _, resolver := range dns["direct-nameserver"].([]string) {
			if !strings.HasSuffix(resolver, "#Main") {
				t.Fatalf("direct traffic must resolve through Main: %s", resolver)
			}
		}
		for _, resolver := range dns["proxy-server-nameserver"].([]string) {
			if !strings.HasSuffix(resolver, "#DIRECT") {
				t.Fatalf("node bootstrap must remain independent of Main: %s", resolver)
			}
		}
		m := dns["nameserver-policy"].(M)
		cnAI := m["+.moonshot.cn"].([]string)
		if policyName == "split" {
			want := []string{"https://223.5.5.5/dns-query#DIRECT", "https://223.6.6.6/dns-query#DIRECT", "https://doh.pub/dns-query#DIRECT"}
			if !slices.Equal(cnAI, want) || !slices.Equal(m["+.doubao.com"].([]string), want) {
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
		if _, exists := m["www.12306.cn"]; exists {
			t.Fatal("individual domestic service DNS rule")
		}
		bootstrap := dns["default-nameserver"].([]string)
		if policyName == "split" {
			if !slices.Equal(bootstrap, []string{"https://223.5.5.5/dns-query", "https://223.6.6.6/dns-query"}) ||
				!slices.Equal(m["geosite:cn"].([]string), via(bootstrap, "DIRECT")) {
				t.Fatal("incorrect split bootstrap or CN DNS")
			}
		} else if !slices.Contains(bootstrap, "https://1.1.1.1/dns-query") || len(bootstrap) != 3 {
			t.Fatal("incorrect strict bootstrap")
		}
		_, has := m["geosite:cn"]
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
		if binary.BigEndian.Uint16(msg[end+1:end+3]) == 1 {
			// A loopback origin lets the mixed proxy complete a real DIRECT request.
			answer = append(answer, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 1, 0, 4, 127, 0, 0, 1)
		} else {
			answer = append(answer, 0xc0, 0x0c, 0, 16, 0, 1, 0, 0, 0, 1, 0, 3, 2, 'o', 'k')
		}
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
	for _, mode := range []string{"split", "domestic-first"} {
		t.Run(mode, func(t *testing.T) { testMihomoDNSPaths(t, mode) })
	}
}

func testMihomoDNSPaths(t *testing.T, mode string) {
	bin := os.Getenv("MIHOMO_BIN")
	if bin == "" {
		t.Skip("requires MIHOMO_BIN")
	}
	foreign, domestic, bootstrap := newDoH(t), newDoH(t), newDoH(t)
	var offline atomic.Bool
	var forwarded, domesticForwarded atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || offline.Load() || (r.Host != foreign.Listener.Addr().String() && r.Host != domestic.Listener.Addr().String()) {
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
		if r.Host == domestic.Listener.Addr().String() {
			domesticForwarded.Add(1)
		} else {
			forwarded.Add(1)
		}
		fmt.Fprint(rw, "HTTP/1.1 200 OK\r\n\r\n")
		rw.Flush()
		go func() { _, _ = io.Copy(up, rw); up.Close() }()
		_, _ = io.Copy(client, up)
	}))
	defer proxy.Close()
	s, _ := model.New("127.0.0.1")
	s.MihomoPolicy = mode
	s.Overrides = []routing.Rule{
		{Kind: "DOMAIN", Value: "priority.jd.com", Group: "AI"},
		{Kind: "DOMAIN", Value: "priority.jd.com", Group: "AI-CN"},
	}
	s.Rules.Rules = []routing.Rule{{Kind: "DOMAIN-KEYWORD", Value: "protected-svc", Group: "AI"}}
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
	explicit := bytes.Index(data, []byte("priority.jd.com:"))
	cn := bytes.Index(data, []byte("geosite:cn:"))
	if explicit < 0 || cn < explicit {
		t.Fatal("exported CN resolver precedes exact DNS override")
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
		if strings.Contains(u, "223.5.5.5") || strings.Contains(u, "223.6.6.6") || strings.Contains(u, "doh.pub") {
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
	dn["nameserver-policy"] = mihomoDNSPolicies(dn["nameserver-policy"].(M))
	// Mihomo v1.19 uses this spelling for process-local additional TLS roots.
	cfg["tls"] = M{"custom-certifactes": []string{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: foreign.Certificate().Raw}))}}
	_, port, _ := net.SplitHostPort(proxy.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	cfg["proxies"] = []M{{"name": "direct-ss", "type": "http", "server": "127.0.0.1", "port": p}}
	for _, entry := range cfg["proxy-groups"].([]any) {
		g := entry.(M)
		if g["name"] == "Unmatched" {
			g["proxies"] = []string{"DIRECT", "Main"}
		}
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
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "direct-origin") }))
	defer origin.Close()
	probeDirect := func(host string) bool {
		t.Helper()
		proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", cfg["mixed-port"]))
		transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
		defer transport.CloseIdleConnections()
		client := http.Client{Transport: transport, Timeout: 8 * time.Second}
		resp, err := client.Get(fmt.Sprintf("http://%s:%d/", host, origin.Listener.Addr().(*net.TCPAddr).Port))
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return string(body) == "direct-origin"
	}
	addr, stop := start()
	directName := "unknown-live.example"
	if !probeDirect(directName) || !foreign.saw(directName) || domestic.saw(directName) || bootstrap.saw(directName) {
		t.Fatal("Unmatched DIRECT did not resolve the origin exclusively through proxied DNS")
	}
	for _, q := range []string{"api.openai.com", "assets.oaistatic.com", "api.anthropic.com", "unclassified-ai.example.org", "protected-svc.jd.com", "priority.jd.com", "api.groq.com", "yuanbao.tencent.com", "www.jd.com", "www.12306.cn", "api.moonshot.cn", "api.deepseek.com"} {
		if !txtQuery("udp4", addr, q) {
			t.Fatalf("no TXT answer for %s", q)
		}
		if q == "www.12306.cn" || q == "www.jd.com" || q == "yuanbao.tencent.com" || q == "api.moonshot.cn" || q == "api.deepseek.com" {
			if !domestic.saw(q) || foreign.saw(q) {
				t.Fatalf("%s: domestic=%t foreign=%t bootstrap=%t", q, domestic.saw(q), foreign.saw(q), bootstrap.saw(q))
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
	if mode == "domestic-first" {
		if domesticForwarded.Load() != 0 {
			t.Fatal("domestic DNS unexpectedly traversed the proxy with AI-CN set to DIRECT")
		}
		for _, entry := range cfg["proxy-groups"].([]any) {
			group := entry.(M)
			if group["name"] == "AI-CN" {
				group["proxies"] = []string{"Main", "DIRECT"}
			}
		}
		addr, stop = start()
		if !txtQuery("udp4", addr, "switch.doubao.com") || !domestic.saw("switch.doubao.com") || domesticForwarded.Load() == 0 {
			t.Fatal("allowlisted domestic AI DNS did not follow AI-CN to Main")
		}
		stop()
		for _, entry := range cfg["proxy-groups"].([]any) {
			group := entry.(M)
			if group["name"] == "AI-CN" || group["name"] == "Unmatched" {
				group["proxies"] = []string{"DIRECT", "Main"}
			}
		}
	}
	offline.Store(true)
	addr, _ = start()
	directName = "unknown-outage.example"
	if probeDirect(directName) || domestic.saw(directName) || bootstrap.saw(directName) {
		t.Fatal("Unmatched DIRECT leaked a lookup or reached the origin during proxy outage")
	}
	for _, q := range []string{"outage.openai.com", "outage.google.com", "unlisted-foreign.example.net", "outage.example.org"} {
		if txtQuery("udp4", addr, q) || domestic.saw(q) || bootstrap.saw(q) || foreign.saw(q) {
			t.Fatalf("DNS failed open after proxy outage for %s", q)
		}
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
	for q, want := range map[string]string{"100.127.255.255": "DIRECT", "sub.msftncsi.com": "DIRECT", "cdn.doubleclick.net": "REJECT", "api.moonshot.cn": "AI-CN", "api.deepseek.com": "AI-CN", "generativelanguage.googleapis.com": "AI", "x.generativelanguage.googleapis.com": "Client", "img.ytimg.com": "Streaming", "telegram.me": "Communication", "media.whatsapp.net": "Communication", "discord.gg": "Communication", "raw.githubusercontent.com": "Main", "other.example": "Client"} {
		if got := Explain(*s, q); !strings.HasPrefix(got, want+" ") {
			t.Errorf("%s: %s", q, got)
		}
	}
	s.Overrides = []routing.Rule{{Kind: "DOMAIN", Value: "fixture.example", Group: "DIRECT"}}
	pre, _ := profilePolicy(*s, true)
	rules := mihomoRules(*s, pre)
	want := []string{"GEOSITE,CN,DIRECT", "GEOIP,CN,DIRECT,no-resolve", "MATCH,Unmatched"}
	if slices.Contains(rules[:len(rules)-3], want[0]) || slices.Contains(rules[:len(rules)-3], want[1]) || !slices.Equal(rules[len(rules)-3:], want) || !slices.Contains(rules, "DOMAIN,fixture.example,DIRECT") {
		t.Fatal("geo/final precedence")
	}
	if strings.Contains(strings.Join(rules, "\n"), "RULE-SET,cn-domains") || strings.Contains(strings.Join(rules, "\n"), "RULE-SET,cn-ips") || len(rules) != len(pre)+3 {
		t.Fatal("expanded geographic payload")
	}
	dns := mihomoDNS(*s, pre, false)
	for host, key := range map[string]string{
		"api2.cursor.sh": "+.cursor.sh", "marketplace.cursorapi.com": "+.cursorapi.com",
		"gemini.gstatic.com": "+.gemini.gstatic.com", "geminiweb-pa.clients6.google.com": "geminiweb-pa.clients6.google.com",
		"antigravity-pa.googleapis.com": "antigravity-pa.googleapis.com", "pplx.ai": "+.pplx.ai",
		"antigravity.google": "+.antigravity.google", "daily-cloudcode-pa.googleapis.com": "daily-cloudcode-pa.googleapis.com",
		"aiplatform.eu.rep.googleapis.com": "aiplatform.eu.rep.googleapis.com", "jnn-pa.googleapis.com": "jnn-pa.googleapis.com",
		"api.githubcopilot.com": "+.githubcopilot.com", "copilot-proxy.githubusercontent.com": "copilot-proxy.githubusercontent.com",
	} {
		if got := Explain(*s, host); !strings.HasPrefix(got, "AI ") {
			t.Errorf("%s routed to %s", host, got)
		}
		upstreams, ok := dns["nameserver-policy"].(M)[key].([]string)
		if !ok || len(upstreams) == 0 || slices.ContainsFunc(upstreams, func(u string) bool { return !strings.HasSuffix(u, "#AI") }) {
			t.Errorf("%s has unexpected DNS policy %v", host, upstreams)
		}
	}
	if !slices.Equal(dns["nameserver-policy"].(M)["fixture.example"].([]string), dns["nameserver"].([]string)) {
		t.Fatal("DIRECT override must resolve via Main by default")
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

func TestSmartAIClientCategoriesAndOverrideOrder(t *testing.T) {
	s, _ := model.New("192.0.2.1")
	s.MihomoPolicy = "split"
	s.Overrides = []routing.Rule{
		{Kind: "DOMAIN", Value: "exact.example", Group: "AI"},
		{Kind: "DOMAIN", Value: "exact.example", Group: "AI-CN"},
		{Kind: "DOMAIN-SUFFIX", Value: "example.net", Group: "AI"},
		{Kind: "DOMAIN", Value: "child.example.net", Group: "AI-CN"},
		{Kind: "DOMAIN", Value: "docs.doubao.com", Group: "Main"},
	}
	pol, _ := profilePolicy(*s, true)
	lines := mihomoRules(*s, pol)
	last := -1
	for _, rule := range []string{
		"DOMAIN,exact.example,AI", "DOMAIN,exact.example,AI-CN", "DOMAIN-SUFFIX,cursor.com,AI",
		"GEOSITE,category-ai-!cn,AI", "GEOSITE,category-ai-cn,AI-CN",
		"DOMAIN-SUFFIX,google.com,Main", "GEOSITE,CN,DIRECT", "GEOIP,CN,DIRECT,no-resolve", "MATCH,Unmatched",
	} {
		idx := slices.Index(lines, rule)
		if idx <= last {
			t.Fatalf("missing or misplaced %s at %d after %d", rule, idx, last)
		}
		last = idx
	}
	dns := mihomoDNS(*s, pol, false)
	m := dns["nameserver-policy"].(M)
	for key, group := range map[string]string{
		"exact.example": "#AI", "child.example.net": "#AI", "docs.doubao.com": "#Main",
		"geosite:category-ai-!cn": "#AI",
	} {
		upstreams, ok := m[key].([]string)
		if !ok || len(upstreams) == 0 || slices.ContainsFunc(upstreams, func(v string) bool { return !strings.HasSuffix(v, group) }) {
			t.Errorf("%s DNS = %v, want %s", key, upstreams, group)
		}
	}
	for _, key := range []string{"geosite:category-ai-cn", "www.jd.com", "+.cn"} {
		if _, exists := m[key]; exists {
			t.Fatalf("unexpected geographic DNS permission %s", key)
		}
	}
	if got := Explain(*s, "www.hosuyun.cn"); !strings.Contains(got, "GeoSite/GeoIP") || !strings.Contains(got, "Unmatched") {
		t.Fatal(got)
	}
}

func TestDomesticFirstDNSBoundaries(t *testing.T) {
	s, _ := model.New("192.0.2.2")
	s.MihomoPolicy = "domestic-first"
	s.Overrides = []routing.Rule{{Kind: "DOMAIN", Value: "foreign.example", Group: "AI-CN"}}
	pol, _ := profilePolicy(*s, true)
	lines := mihomoRules(*s, pol)
	want := []string{"GEOSITE,CN,DIRECT", "GEOSITE,geolocation-!cn,Main", "GEOIP,CN,DIRECT,no-resolve", "MATCH,Unmatched"}
	if !slices.Equal(lines[len(lines)-4:], want) {
		t.Fatalf("wrong terminal rules: %v", lines)
	}
	for _, tc := range []struct{ family, transport, first string }{
		{"ipv4", "https", "https://223.5.5.5/dns-query"},
		{"ipv6", "tls", "tls://[2400:3200::1]:853"},
	} {
		s.DNSFamily, s.DNSTransport = tc.family, tc.transport
		d := mihomoDNS(*s, pol, tc.family == "ipv6")
		m := d["nameserver-policy"].(M)
		if d["default-nameserver"].([]string)[0] != tc.first || m["+.doubao.com"].([]string)[0] != tc.first+"#AI-CN" ||
			m["geosite:cn"].([]string)[0] != tc.first+"#DIRECT" {
			t.Fatalf("wrong %s resolver or exit", tc.family)
		}
		if d["respect-rules"] != false || d["direct-nameserver-follow-policy"] != true || d["fallback"] != nil ||
			d["use-system-hosts"] != false || !slices.Equal(d["direct-nameserver"].([]string), d["nameserver"].([]string)) {
			t.Fatal("unsafe default DNS path")
		}
		for _, r := range d["nameserver"].([]string) {
			if !strings.HasSuffix(r, "#Main") {
				t.Fatal(r)
			}
		}
		if !slices.Equal(m["foreign.example"].([]string), m["+.openai.com"].([]string)) {
			t.Fatal("business routing override disclosed an unknown domain to domestic DNS")
		}
		for _, k := range []string{"+.cn", "www.12306.cn", "geosite:" + geoAICN, "+.byteimg.com"} {
			if _, ok := m[k]; ok {
				t.Fatalf("domestic DNS authorized by %s", k)
			}
		}
	}
}
