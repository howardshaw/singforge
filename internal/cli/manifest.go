package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

type manifest struct {
	Preset, Mode, Host, Listen, Handshake string
	Protocols                             []string
	Endpoints                             struct{ Direct, CDN, Tunnel string }
	Origin                                struct {
		Cert, Key string
		Port      int
	}
	Credentials string
}

func loadManifest(path string, f *pflag.FlagSet) error {
	file, e := os.Open(path)
	if e != nil {
		return e
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
		return fmt.Errorf("expected a regular configuration file under 64 KiB")
	}
	dec := yaml.NewDecoder(file)
	dec.KnownFields(true)
	var m manifest
	if e = dec.Decode(&m); e != nil {
		return fmt.Errorf("invalid installation manifest: %w", e)
	}
	var extra any
	if e = dec.Decode(&extra); e != io.EOF {
		return fmt.Errorf("expected one YAML document")
	}
	host := m.Host
	if m.Endpoints.Direct != "" {
		if host != "" && host != m.Endpoints.Direct {
			return fmt.Errorf("conflicting direct hosts in manifest")
		}
		host = m.Endpoints.Direct
	}
	values := map[string]string{"preset": m.Preset, "mode": m.Mode, "host": host, "listen": m.Listen, "handshake": m.Handshake, "protocols": strings.Join(m.Protocols, ","), "cdn": m.Endpoints.CDN, "tunnel": m.Endpoints.Tunnel}
	for k, v := range map[string]string{"key": m.Origin.Key, "cert": m.Origin.Cert, "credentials": m.Credentials} {
		if v != "" {
			if !filepath.IsAbs(v) {
				v = filepath.Join(filepath.Dir(path), v)
			}
			values[k] = v
		}
	}
	if m.Origin.Port != 0 {
		values["port"] = fmt.Sprint(m.Origin.Port)
	}
	for k, v := range values {
		if v != "" && !f.Changed(k) {
			if e = f.Set(k, v); e != nil {
				return e
			}
		}
	}
	return nil
}
