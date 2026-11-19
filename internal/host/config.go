package host

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/render"
	"github.com/howardshaw/singforge/internal/store"
	"github.com/howardshaw/singforge/internal/subscription"
)

const coreUnit = "singforge-core.service"
const edgeUnit = "singforge-edge.service"
const subUnit = "singforge-subscription.service"
const tunnelUnit = "singforge-tunnel.service"

const rulesService = "singforge-rules.service"
const rulesTimer = "singforge-rules.timer"

//go:embed assets/rules.service
var ruleServiceFile []byte

//go:embed assets/rules.timer
var ruleTimerFile []byte
var all = []string{coreUnit, tunnelUnit, subUnit, edgeUnit, rulesService, rulesTimer}
var uuid = regexp.MustCompile(`^[a-fA-F0-9]{8}(-[a-fA-F0-9]{4}){3}-[a-fA-F0-9]{12}$`)
var pathRE = regexp.MustCompile(`^/[a-f0-9]{32}$`)

func CheckPair(host, cert, key string) error {
	pair, e := tls.X509KeyPair([]byte(cert), []byte(key))
	if e != nil {
		return fmt.Errorf("invalid certificate/key pair")
	}
	c, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil {
		return e
	}
	if time.Now().Before(c.NotBefore) || time.Now().After(c.NotAfter) {
		return fmt.Errorf("certificate outside validity interval")
	}
	return c.VerifyHostname(host)
}
func CheckTrust(host, cert, key, ca string) error {
	pair, e := tls.X509KeyPair([]byte(cert), []byte(key))
	if e != nil {
		return fmt.Errorf("invalid HTTPS certificate/key pair")
	}
	leaf, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil {
		return e
	}
	opts := x509.VerifyOptions{DNSName: host, Intermediates: x509.NewCertPool()}
	if ca != "" {
		opts.Roots = x509.NewCertPool()
		if !opts.Roots.AppendCertsFromPEM([]byte(ca)) {
			return fmt.Errorf("invalid CA certificate")
		}
	}
	for _, der := range pair.Certificate[1:] {
		c, e := x509.ParseCertificate(der)
		if e != nil {
			return e
		}
		opts.Intermediates.AddCert(c)
	}
	_, e = leaf.Verify(opts)
	return e
}
func TunnelID(data string) (string, error) {
	var v struct{ TunnelID, TunnelSecret, AccountTag string }
	if json.Unmarshal([]byte(data), &v) != nil || !uuid.MatchString(v.TunnelID) || v.AccountTag == "" {
		return "", fmt.Errorf("invalid named tunnel credentials")
	}
	d, e := base64.StdEncoding.DecodeString(v.TunnelSecret)
	if e != nil || len(d) < 32 {
		return "", fmt.Errorf("invalid tunnel secret")
	}
	return v.TunnelID, nil
}
func unit(name, cmd string, files ...string) []byte {
	b := "[Unit]\nDescription=SingForge " + name + "\nAfter=network-online.target\nWants=network-online.target\n[Service]\nDynamicUser=yes\nNoNewPrivileges=yes\nProtectSystem=strict\nProtectHome=yes\nPrivateTmp=yes\nProtectKernelTunables=yes\nProtectControlGroups=yes\nRestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK\nUMask=0077\nRestart=on-failure\nRestartSec=3\n"
	for _, f := range files {
		b += "LoadCredential=" + f + ":" + store.Root + "/current/" + f + "\n"
	}
	if name == "edge" || name == "core" {
		b += "AmbientCapabilities=CAP_NET_BIND_SERVICE\nCapabilityBoundingSet=CAP_NET_BIND_SERVICE\n"
	} else {
		b += "CapabilityBoundingSet=\n"
	}
	if name == "edge" {
		b += "RuntimeDirectory=singforge-edge\n"
	}
	return []byte(b + "ExecStart=" + cmd + "\n[Install]\nWantedBy=multi-user.target\n")
}
func Files(s model.State, core, cloud string) (map[string][]byte, error) {
	if e := s.Validate(); e != nil {
		return nil, e
	}
	if e := CheckPair(s.ServerName(), s.Certificate, s.Key); e != nil {
		return nil, e
	}
	cfg, e := render.Server(s)
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(s)
	if e != nil {
		return nil, e
	}
	files := map[string][]byte{"state.json": raw, "server.json": cfg, coreUnit: unit("core", core+" run -c %d/server.json", "server.json")}
	var locations, ng strings.Builder
	var ingress []render.M
	for _, n := range s.Nodes() {
		if !n.Spec.WS {
			continue
		}
		if !pathRE.MatchString(n.Auth.Path) {
			return nil, fmt.Errorf("invalid WebSocket path")
		}
		if n.Mode == "cdn" {
			if n.Host != s.CDN || n.Port != s.CDNPort {
				return nil, fmt.Errorf("CDN endpoint differs from origin")
			}
			fmt.Fprintf(&locations, "location = %s { proxy_pass http://127.0.0.1:%d; proxy_http_version 1.1; proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection upgrade; proxy_read_timeout 1h; }\n", n.Auth.Path, n.Auth.Port)
		} else {
			if n.Host != s.Tunnel || n.Port != 443 {
				return nil, fmt.Errorf("tunnel endpoint differs from ingress")
			}
			ingress = append(ingress, render.M{"hostname": s.Tunnel, "path": "^" + n.Auth.Path + "$", "service": fmt.Sprintf("http://127.0.0.1:%d", n.Auth.Port)})
		}
	}
	const creds = "/run/credentials/" + edgeUnit + "/"
	edgeFiles := []string{"nginx.conf"}
	addSite := func(host string, port int, cert, key, id, body string) error {
		if !model.Domain(host) {
			return fmt.Errorf("invalid HTTPS hostname")
		}
		if e := CheckPair(host, cert, key); e != nil {
			return e
		}
		files[id+".crt"] = []byte(cert)
		files[id+".key"] = []byte(key)
		edgeFiles = append(edgeFiles, id+".crt", id+".key")
		fmt.Fprintf(&ng, "server { listen %d ssl; listen [::]:%d ssl; server_name %s; ssl_certificate %s%s.crt; ssl_certificate_key %s%s.key; if ($host != %s) { return 444; } %s }\n", port, port, host, creds, id, creds, id, host, body)
		return nil
	}
	if locations.Len() > 0 {
		if !slices.Contains([]int{443, 2053, 2083, 2087, 2096, 8443}, s.CDNPort) {
			return nil, fmt.Errorf("unsupported CDN HTTPS port")
		}
		if e := addSite(s.CDN, s.CDNPort, s.OriginCert, s.OriginKey, "origin", locations.String()+"location / { return 404; }"); e != nil {
			return nil, e
		}
	}
	if s.PublishHost != "" {
		if s.AutoRenew {
			files["renewal-lineage"] = []byte(Lineage(s.PublishHost) + "\n")
		}
		if e := CheckTrust(s.PublishHost, s.PublishCert, s.PublishKey, s.PublishCA); e != nil {
			return nil, fmt.Errorf("subscription TLS trust failed: %w", e)
		}
		if s.PublishHost == s.CDN {
			return nil, fmt.Errorf("use a distinct subscription hostname")
		}
		if e := addSite(s.PublishHost, 443, s.PublishCert, s.PublishKey, "subscription", "location /s/ { proxy_set_header Host "+s.PublishHost+"; proxy_pass http://127.0.0.1:19781; } location / { return 404; }"); e != nil {
			return nil, e
		}
		files["bundle.json"], e = json.Marshal(subscription.Build(s))
		if e != nil {
			return nil, e
		}
		files[subUnit] = []byte(strings.Replace(string(unit("subscription", Bin+"/singforge serve -")), "[Service]", "[Service]\nStandardInput=file:"+store.Root+"/current/bundle.json", 1))
	}
	if ng.Len() > 0 {
		for _, n := range s.Inbounds {
			sp, _ := model.Lookup(n.Protocol)
			if n.Enabled && slices.Contains(sp.Networks(), "tcp") && (n.Port == 443 && s.PublishHost != "" || locations.Len() > 0 && n.Port == s.CDNPort || s.PublishHost != "" && n.Port == 19781) {
				return nil, fmt.Errorf("edge port conflicts with %s; configure its port first", n.Protocol)
			}
		}
		files["nginx.conf"] = []byte("daemon off; worker_processes 1; pid /run/singforge-edge/nginx.pid; error_log /dev/null; events {} http { access_log off; ssl_protocols TLSv1.2 TLSv1.3; client_max_body_size 1k; client_body_temp_path /run/singforge-edge/body; proxy_temp_path /run/singforge-edge/proxy; fastcgi_temp_path /run/singforge-edge/fastcgi; uwsgi_temp_path /run/singforge-edge/uwsgi; scgi_temp_path /run/singforge-edge/scgi;\n" + ng.String() + "}")
		files[edgeUnit] = unit("edge", "/usr/sbin/nginx -c %d/nginx.conf", edgeFiles...)
	}
	if len(ingress) > 0 {
		id, e := TunnelID(s.TunnelCredentials)
		if s.ConnectorToken != "" {
			id, e = ConnectorID(s.ConnectorToken)
		}
		if e != nil || id != s.TunnelID {
			return nil, fmt.Errorf("missing or mismatched tunnel credentials")
		}
		ingress = append(ingress, render.M{"service": "http_status:404"})
		files["tunnel.yml"], e = yaml.Marshal(render.M{"tunnel": id, "credentials-file": "/run/credentials/" + tunnelUnit + "/tunnel.json", "metrics": "127.0.0.1:19782", "ingress": ingress})
		if e != nil {
			return nil, e
		}
		if s.ConnectorToken != "" {
			files["connector.token"] = []byte(s.ConnectorToken)
			files[tunnelUnit] = unit("tunnel", cloud+" --no-autoupdate tunnel --metrics 127.0.0.1:19782 run --token-file %d/connector.token", "connector.token")
		} else {
			files["tunnel.json"] = []byte(s.TunnelCredentials)
			files[tunnelUnit] = unit("tunnel", cloud+" --no-autoupdate tunnel --config %d/tunnel.yml run", "tunnel.yml", "tunnel.json")
		}
	}
	if s.ScheduleRules {
		files[rulesService] = ruleServiceFile
		files[rulesTimer] = ruleTimerFile
	}
	return files, nil
}
func DetectAddress() (string, error) {
	ips, e := net.InterfaceAddrs()
	if e != nil {
		return "", e
	}
	for _, a := range ips {
		ip, _, _ := net.ParseCIDR(a.String())
		if ip.IsGlobalUnicast() && !ip.IsPrivate() {
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("no public interface address; supply --host <public IP or hostname>")
}

// ConnectorID verifies the envelope of a dashboard-issued connector token.
func ConnectorID(token string) (string, error) {
	raw, e := base64.StdEncoding.DecodeString(token)
	if e != nil {
		raw, e = base64.RawStdEncoding.DecodeString(token)
	}
	var envelope struct {
		Account string `json:"a"`
		Tunnel  string `json:"t"`
		Secret  []byte `json:"s"`
	}
	if e != nil || json.Unmarshal(raw, &envelope) != nil || !uuid.MatchString(envelope.Tunnel) || len(envelope.Account) != 32 || len(envelope.Secret) < 32 {
		return "", fmt.Errorf("invalid Cloudflare connector token")
	}
	return envelope.Tunnel, nil
}
