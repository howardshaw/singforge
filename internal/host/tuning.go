package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/howardshaw/singforge/internal/store"
)

const sysctlFile = "/etc/sysctl.d/90-singforge.conf"

type Knob struct{ Name, Before, After string }

func procPath(name string) string { return "/proc/sys/" + strings.ReplaceAll(name, ".", "/") }
func sysValue(name string) (string, error) {
	b, e := os.ReadFile(procPath(name))
	return strings.TrimSpace(string(b)), e
}
func TunePlan(profile string) ([]Knob, error) {
	list, e := sysValue("net.ipv4.tcp_available_congestion_control")
	if e != nil {
		return nil, e
	}
	if !slices.Contains(strings.Fields(list), "bbr") {
		return nil, fmt.Errorf("kernel has no available BBR algorithm")
	}
	data, e := os.ReadFile("/proc/meminfo")
	if e != nil {
		return nil, e
	}
	var kib int64
	if _, e = fmt.Sscanf(string(data), "MemTotal: %d kB", &kib); e != nil {
		return nil, e
	}
	return knobs(profile, kib, sysValue)
}
func knobs(profile string, kib int64, read func(string) (string, error)) ([]Knob, error) {
	var result []Knob
	keys := []string{"net.ipv4.tcp_congestion_control"}
	vals := []string{"bbr"}
	switch profile {
	case "", "default":
	case "fq", "streaming":
		keys = append(keys, "net.core.default_qdisc")
		vals = append(vals, "fq")
	default:
		return nil, fmt.Errorf("use default, fq or streaming profile")
	}
	if profile == "streaming" {
		n := max(int64(256<<10), min(int64(16<<20), kib*1024/256))
		for _, k := range []string{"net.core.rmem_max", "net.core.wmem_max"} {
			keys = append(keys, k)
			vals = append(vals, strconv.FormatInt(n, 10))
		}
	}
	for i, k := range keys {
		v, e := read(k)
		if e != nil {
			return nil, e
		}
		if strings.HasSuffix(k, "mem_max") {
			old, e := strconv.ParseInt(v, 10, 64)
			if e != nil {
				return nil, e
			}
			n, _ := strconv.ParseInt(vals[i], 10, 64)
			if old >= n {
				continue
			}
		}
		if v != vals[i] {
			result = append(result, Knob{k, v, vals[i]})
		}
	}
	return result, nil
}
func conf(v []Knob) []byte {
	var b strings.Builder
	b.WriteString("# Owned by SingForge\n")
	for _, k := range v {
		fmt.Fprintf(&b, "%s = %s\n", k.Name, k.After)
	}
	return []byte(b.String())
}
func Tune(dir, action, profile string, dry, yes bool) (any, error) {
	path := filepath.Join(dir, "tuning.json")
	if action == "status" {
		m := map[string]string{}
		for _, k := range []string{"net.ipv4.tcp_congestion_control", "net.ipv4.tcp_available_congestion_control", "net.core.default_qdisc", "net.core.rmem_max", "net.core.wmem_max"} {
			m[k], _ = sysValue(k)
		}
		return m, nil
	}
	old, e := os.ReadFile(path)
	if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	if action == "disable" {
		if os.IsNotExist(e) {
			return nil, nil
		}
		var v []Knob
		if e = json.Unmarshal(old, &v); e != nil {
			return nil, e
		}
		if dry {
			return v, nil
		}
		if !yes {
			return nil, fmt.Errorf("confirm restoration with --yes")
		}
		b, e := os.ReadFile(sysctlFile)
		if e != nil && !os.IsNotExist(e) {
			return nil, e
		}
		if b != nil && !bytes.Equal(b, conf(v)) {
			return nil, fmt.Errorf("owned sysctl configuration has changed")
		}
		for _, k := range v {
			x, e := sysValue(k.Name)
			if e != nil {
				return nil, e
			}
			if x != k.Before && x != k.After {
				return nil, fmt.Errorf("kernel value changed independently: %s", k.Name)
			}
		}
		for _, k := range v {
			if e = os.WriteFile(procPath(k.Name), []byte(k.Before), 0600); e != nil {
				return nil, e
			}
		}
		if b != nil {
			if e = os.Remove(sysctlFile); e != nil {
				return nil, e
			}
		}
		return v, os.Remove(path)
	}
	if old != nil {
		return nil, fmt.Errorf("tuning already recorded; disable before reconfiguring")
	}
	v, e := TunePlan(profile)
	if e != nil {
		return nil, e
	}
	if len(v) == 0 {
		return v, nil
	}
	if _, e = os.Lstat(sysctlFile); !os.IsNotExist(e) {
		return nil, fmt.Errorf("existing sysctl file conflict")
	}
	for _, pattern := range []string{"/etc/sysctl.conf", "/etc/sysctl.d/*.conf", "/run/sysctl.d/*.conf", "/usr/lib/sysctl.d/*.conf", "/usr/local/lib/sysctl.d/*.conf"} {
		files, _ := filepath.Glob(pattern)
		for _, p := range files {
			b, e := os.ReadFile(p)
			if e != nil {
				return nil, e
			}
			for line := range strings.SplitSeq(string(b), "\n") {
				name, _, ok := strings.Cut(line, "=")
				if !ok {
					continue
				}
				name = strings.ReplaceAll(strings.TrimLeft(strings.TrimSpace(name), "-"), "/", ".")
				for _, k := range v {
					match, _ := filepath.Match(name, k.Name)
					if match {
						return nil, fmt.Errorf("%s already configures %s", p, k.Name)
					}
				}
			}
		}
	}
	if dry {
		return v, nil
	}
	if !yes {
		return v, fmt.Errorf("review with --dry-run then apply with --yes")
	}
	data, _ := json.Marshal(v)
	if e = store.Write(path, data, 0600); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(sysctlFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if e != nil {
		os.Remove(path)
		return nil, e
	}
	_, e = f.Write(conf(v))
	e = errors.Join(e, f.Sync(), f.Close())
	if e == nil {
		for _, k := range v {
			if e = os.WriteFile(procPath(k.Name), []byte(k.After), 0600); e != nil {
				break
			}
		}
	}
	if e != nil {
		_, undo := Tune(dir, "disable", "", false, true)
		return nil, errors.Join(e, undo)
	}
	return v, nil
}
