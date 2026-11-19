package model

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/howardshaw/singforge/internal/routing"
)

const Schema = 1

type Spec struct {
	ID, Type string
	Port     int
	WS, TLS  bool
}

var Catalog = []Spec{
	{"reality", "vless", 443, false, false}, {"hysteria2", "hysteria2", 8443, false, true}, {"ss", "shadowsocks", 8388, false, false}, {"ss2022", "shadowsocks", 8389, false, false}, {"tuic", "tuic", 8444, false, true}, {"trojan", "trojan", 8445, false, true}, {"anytls", "anytls", 8446, false, true}, {"snell", "snell", 8447, false, false}, {"naive", "naive", 8448, false, true}, {"reality-grpc", "vless", 8449, false, false}, {"shadowtls", "shadowtls", 8450, false, false}, {"vless-ws", "vless", 11001, true, false}, {"trojan-ws", "trojan", 11002, true, false}, {"vmess-ws", "vmess", 11003, true, false},
}

func Lookup(id string) (Spec, bool) {
	for _, s := range Catalog {
		if s.ID == id {
			return s, true
		}
	}
	return Spec{}, false
}

type Inbound struct {
	Protocol, UUID, Password, Private, Public, ShortID, Path string
	Port                                                     int
	Enabled                                                  bool
}
type Endpoint struct {
	ID, Inbound, Mode, Host string
	Port                    int
}
type Node struct {
	Endpoint
	Spec                        Spec
	Auth                        Inbound
	Certificate, SNI, Handshake string
	PublicTLS                   bool
}
type Grant struct {
	Digest  string
	Expires time.Time
}
type State struct {
	MihomoIPv6                                                                               string `json:",omitempty"`
	MihomoPolicy                                                                             string `json:",omitempty"`
	AutoRenew                                                                                bool   `json:",omitempty"`
	ConnectorToken                                                                           string `json:",omitempty"`
	RealitySNI, RealityTarget                                                                string
	ScheduleRules                                                                            bool
	ServerDNSFamily                                                                          string
	PublishCA                                                                                string
	TLSName, CloudDigest, PreviousCloudDigest                                                string
	DNSFamily, DNSTransport, Capture                                                         string
	CoreDigest, PreviousCoreDigest                                                           string
	Rules, PreviousRules                                                                     routing.Snapshot
	Overrides                                                                                []routing.Rule
	Version                                                                                  int
	Listen, Direct, CDN, Tunnel, Handshake, Certificate, Key, DNS, Resolver, Routing         string
	CDNPort                                                                                  int
	Inbounds                                                                                 []Inbound
	Endpoints                                                                                []Endpoint
	Grants                                                                                   []Grant
	PublishHost, PublishCert, PublishKey, OriginCert, OriginKey, TunnelID, TunnelCredentials string
	Zone, ZoneID, AccountID                                                                  string
	Records                                                                                  map[string]string
}

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var label = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func Host(s string) bool {
	if net.ParseIP(s) != nil {
		return true
	}
	if len(s) > 253 || s == "" {
		return false
	}
	for _, part := range strings.Split(s, ".") {
		if !label.MatchString(part) {
			return false
		}
	}
	return true
}
func Domain(s string) bool { return Host(s) && net.ParseIP(s) == nil && strings.Contains(s, ".") }
func Random(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
func New(direct string) (*State, error) {
	k, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return nil, e
	}
	now := time.Now()
	c := x509.Certificate{SerialNumber: new(big.Int).SetBytes(Random(16)), Subject: pkix.Name{CommonName: "singforge.invalid"}, DNSNames: []string{"singforge.invalid"}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(1, 0, 0), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, e := x509.CreateCertificate(rand.Reader, &c, &c, &k.PublicKey, k)
	if e != nil {
		return nil, e
	}
	kb, e := x509.MarshalECPrivateKey(k)
	if e != nil {
		return nil, e
	}
	return &State{Version: Schema, Direct: direct, Listen: "::", Handshake: "www.microsoft.com", Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), Key: string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})), DNS: "strict", Resolver: "cloudflare", Routing: "smart", CDNPort: 443, Records: map[string]string{}}, nil
}
func (s *State) Enable(id string) (*Inbound, error) {
	sp, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("unknown protocol %q", id)
	}
	for i := range s.Inbounds {
		if s.Inbounds[i].Protocol == id {
			s.Inbounds[i].Enabled = true
			return &s.Inbounds[i], nil
		}
	}
	u := Random(16)
	u[6] = u[6]&15 | 64
	u[8] = u[8]&63 | 128
	k, e := ecdh.X25519().GenerateKey(rand.Reader)
	if e != nil {
		return nil, e
	}
	b := Inbound{Protocol: id, Enabled: true, Port: sp.Port, UUID: fmt.Sprintf("%x-%x-%x-%x-%x", u[:4], u[4:6], u[6:8], u[8:10], u[10:]), Password: base64.StdEncoding.EncodeToString(Random(32)), Private: base64.RawURLEncoding.EncodeToString(k.Bytes()), Public: base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), ShortID: hex.EncodeToString(Random(8)), Path: "/" + hex.EncodeToString(Random(16))}
	s.Inbounds = append(s.Inbounds, b)
	return &s.Inbounds[len(s.Inbounds)-1], nil
}
func (s *State) Add(id, mode, host string, port int) error {
	sp, ok := Lookup(id)
	if !ok || !Host(host) || port < 1 || port > 65535 {
		return fmt.Errorf("invalid endpoint protocol, host or port")
	}
	if mode == "direct" && sp.WS || mode != "direct" && (!sp.WS || !Domain(host)) || !slices.Contains([]string{"direct", "cdn", "tunnel"}, mode) {
		return fmt.Errorf("unsupported endpoint transport")
	}
	name := mode + "-" + id
	ep := Endpoint{name, id, mode, host, port}
	for i, e := range s.Endpoints {
		if e.ID == name {
			s.Endpoints[i] = ep
			return nil
		}
	}
	s.Endpoints = append(s.Endpoints, ep)
	return nil
}
func (s State) Nodes() []Node {
	var out []Node
	for _, e := range s.Endpoints {
		for _, i := range s.Inbounds {
			if i.Enabled && i.Protocol == e.Inbound {
				sp, _ := Lookup(i.Protocol)
				sni := e.Host
				if sp.TLS {
					sni = s.ServerName()
				}
				handshake := s.Handshake
				if strings.HasPrefix(sp.ID, "reality") {
					handshake = s.RealityName()
				}
				out = append(out, Node{e, sp, i, s.Certificate, sni, handshake, s.TLSName != ""})
			}
		}
	}
	return out
}
func (s State) Validate() error {
	if s.Version != Schema || net.ParseIP(s.Listen) == nil || !Host(s.Direct) || !Domain(s.Handshake) {
		return fmt.Errorf("invalid state version, listen address, endpoint or handshake hostname")
	}
	if s.CDN != "" && (s.CDN == s.Direct || s.CDN == s.PublishHost) || s.Tunnel != "" && (s.Tunnel == s.Direct || s.Tunnel == s.CDN || s.Tunnel == s.PublishHost) {
		return fmt.Errorf("conflicting access modes for public hostname")
	}
	target, targetPort, err := s.RealityDestination()
	if err != nil {
		return err
	}
	if !Domain(s.RealityName()) {
		return fmt.Errorf("invalid Reality server name")
	}
	ports := map[string]bool{}
	ids := map[string]bool{}
	for _, i := range s.Inbounds {
		sp, ok := Lookup(i.Protocol)
		if !ok || ids[i.Protocol] || i.Port < 1 || i.Port > 65535 {
			return fmt.Errorf("duplicate or invalid inbound %s / port %d", i.Protocol, i.Port)
		}
		password, err := base64.StdEncoding.DecodeString(i.Password)
		if err != nil || len(password) != 32 || !uuidV4.MatchString(i.UUID) {
			return fmt.Errorf("invalid authentication material for %s", i.Protocol)
		}
		if strings.HasPrefix(i.Protocol, "reality") {
			raw, e := base64.RawURLEncoding.DecodeString(i.Private)
			if e != nil {
				return fmt.Errorf("invalid Reality key")
			}
			key, e := ecdh.X25519().NewPrivateKey(raw)
			if e != nil || base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()) != i.Public {
				return fmt.Errorf("mismatched Reality key pair")
			}
			sid, e := hex.DecodeString(i.ShortID)
			if e != nil || len(sid) != 8 {
				return fmt.Errorf("invalid Reality short ID")
			}
		}
		ids[i.Protocol] = true
		if i.Enabled {
			for _, network := range sp.Networks() {
				k := fmt.Sprintf("%s/%d", network, i.Port)
				if ports[k] {
					return fmt.Errorf("duplicate listener %s", k)
				}
				ports[k] = true
			}
			if strings.HasPrefix(sp.ID, "reality") && targetPort == i.Port && (net.ParseIP(target).IsLoopback() || target == "localhost" || target == s.Direct || target == s.Listen) {
				return fmt.Errorf("handshake target would reconnect to the Reality listener")
			}

		}
	}
	seen := map[string]bool{}
	for _, ep := range s.Endpoints {
		if seen[ep.ID] {
			return fmt.Errorf("duplicate endpoint ID")
		}
		seen[ep.ID] = true
		if !ids[ep.Inbound] {
			return fmt.Errorf("orphan endpoint")
		}
		tmp := State{}
		if err := tmp.Add(ep.Inbound, ep.Mode, ep.Host, ep.Port); err != nil {
			return err
		}
		if !label.MatchString(ep.ID) {
			return fmt.Errorf("invalid endpoint ID")
		}
	}
	for _, rules := range [][]routing.Rule{s.Rules.Rules, s.Overrides} {
		for _, r := range rules {
			if e := r.Check(); e != nil {
				return e
			}
		}
	}
	if !slices.Contains([]string{"", "ipv4", "ipv6"}, s.ServerDNSFamily) || !slices.Contains([]string{"", "ipv4", "ipv6"}, s.DNSFamily) || !slices.Contains([]string{"", "https", "tls"}, s.DNSTransport) || !slices.Contains([]string{"", "system", "tun"}, s.Capture) {
		return fmt.Errorf("unsupported DNS family, capture or transport")
	}
	if !slices.Contains([]string{"", "on", "off"}, s.MihomoIPv6) {
		return fmt.Errorf("unknown Mihomo IPv6 setting")
	}
	if !slices.Contains([]string{"", "strict", "split"}, s.MihomoPolicy) {
		return fmt.Errorf("unknown Mihomo DNS policy")
	}
	if !slices.Contains([]string{"strict", "balanced"}, s.DNS) || !slices.Contains([]string{"google", "cloudflare", "quad9"}, s.Resolver) || !slices.Contains([]string{"global", "smart", "gfw-only", "custom"}, s.Routing) {
		return fmt.Errorf("unknown DNS or routing profile")
	}
	return nil
}

func (s State) ServerName() string {
	if s.TLSName != "" {
		return s.TLSName
	}
	return "singforge.invalid"
}

func (p Spec) Networks() []string {
	switch p.Type {
	case "tuic", "hysteria2":
		return []string{"udp"}
	case "shadowsocks":
		return []string{"tcp", "udp"}
	default:
		return []string{"tcp"}
	}
}
func (s State) RealityName() string {
	if s.RealitySNI != "" {
		return s.RealitySNI
	}
	return s.Handshake
}
func (s State) RealityDestination() (string, int, error) {
	if s.RealityTarget == "" {
		return s.RealityName(), 443, nil
	}
	h, port, e := net.SplitHostPort(s.RealityTarget)
	n, err := strconv.Atoi(port)
	if e != nil || err != nil || !Host(h) || n < 1 || n > 65535 {
		return "", 0, fmt.Errorf("expected Reality target host:port (brackets for IPv6)")
	}
	return h, n, nil
}
