// Optional integration with an existing IPv4 iptables-persistent installation.
package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/store"
)

type Port struct {
	Protocol string
	Number   int
}

func (p Port) args() []string {
	return []string{"-p", p.Protocol, "-m", p.Protocol, "--dport", strconv.Itoa(p.Number), "-m", "comment", "--comment", "singforge-ingress", "-j", "ACCEPT"}
}
func ingress(s model.State) []Port {
	var ports []Port
	add := func(p Port) {
		if !slices.Contains(ports, p) {
			ports = append(ports, p)
		}
	}
	for _, a := range s.Inbounds {
		spec, _ := model.Lookup(a.Protocol)
		if !a.Enabled || spec.WS || s.Listen == "127.0.0.1" || s.Listen == "::1" {
			continue
		}
		for _, proto := range spec.Networks() {
			add(Port{proto, a.Port})
		}
	}
	for _, ep := range s.Nodes() {
		if ep.Mode == "cdn" {
			add(Port{"tcp", s.CDNPort})
			break
		}
	}
	if s.PublishHost != "" {
		add(Port{"tcp", 443})
	}
	return ports
}
func persistentRules(original []byte, previous, wanted []Port) ([]byte, error) {
	strip := map[string]bool{}
	for _, p := range previous {
		strip["-A INPUT "+strings.Join(p.args(), " ")] = true
	}
	lines := strings.Split(string(original), "\n")
	var result []string
	inFilter, added := false, false
	for _, line := range lines {
		if line == "*filter" {
			inFilter = true
		}
		if inFilter {
			normalized := strings.ReplaceAll(line, "\"singforge-ingress\"", "singforge-ingress")
			if strip[normalized] {
				continue
			}
			if !added && (line == "COMMIT" || line == "-A INPUT -j DROP" || strings.HasPrefix(line, "-A INPUT -j REJECT")) {
				for _, p := range wanted {
					result = append(result, "-A INPUT "+strings.Join(p.args(), " "))
				}
				added = true
			}
			if line == "COMMIT" {
				inFilter = false
			}
		}
		result = append(result, line)
	}
	if !added {
		return nil, fmt.Errorf("missing persistent filter table")
	}
	return []byte(strings.Join(result, "\n")), nil
}
func Firewall(dir string, s model.State, remove, dry, yes bool) ([]Port, error) {
	ledger := filepath.Join(dir, "firewall.json")
	b, e := os.ReadFile(ledger)
	var old []Port
	if e == nil {
		if e = json.Unmarshal(b, &old); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	} else if remove {
		return nil, nil
	}
	var want []Port
	if !remove {
		want = ingress(s)
	}
	for _, p := range old {
		if !slices.Contains([]string{"tcp", "udp"}, p.Protocol) || p.Number < 1 || p.Number > 65535 {
			return nil, fmt.Errorf("invalid firewall ledger")
		}
	}
	const path = "/etc/iptables/rules.v4"
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("requires an existing regular iptables-persistent IPv4 rules file")
	}
	before, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	after, e := persistentRules(before, old, want)
	if e != nil {
		return nil, e
	}
	validate := exec.Command("iptables-restore", "--test")
	validate.Stdin = bytes.NewReader(after)
	if e = validate.Run(); e != nil {
		return nil, fmt.Errorf("candidate iptables configuration rejected")
	}
	if dry {
		return want, nil
	}
	if !yes {
		return want, fmt.Errorf("use --yes to apply ingress changes")
	}
	pending := slices.Clone(old)
	for _, p := range want {
		if !slices.Contains(pending, p) {
			pending = append(pending, p)
		}
	}
	b, _ = json.Marshal(pending)
	if e = store.Write(ledger, b, 0600); e != nil {
		return nil, e
	}
	for _, p := range pending {
		check := append([]string{"-w", "5", "-C", "INPUT"}, p.args()...)
		err := exec.Command("iptables", check...).Run()
		var exit *exec.ExitError
		if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
			return nil, fmt.Errorf("cannot inspect IPv4 filter")
		}
		exists := err == nil
		needed := slices.Contains(want, p)
		if exists == needed {
			continue
		}
		args := []string{"-w", "5", "-D", "INPUT"}
		if needed {
			rules, e := exec.Command("iptables", "-w", "5", "-S", "INPUT").Output()
			if e != nil {
				return nil, e
			}
			at := 1
			for line := range strings.SplitSeq(string(rules), "\n") {
				if line == "-A INPUT -j DROP" || strings.HasPrefix(line, "-A INPUT -j REJECT") {
					break
				}
				if strings.HasPrefix(line, "-A INPUT ") {
					at++
				}
			}
			args = []string{"-w", "5", "-I", "INPUT", strconv.Itoa(at)}
		}
		if e = run("iptables", append(args, p.args()...)...); e != nil {
			return nil, e
		}
	}
	curr, e := os.ReadFile(path)
	if e != nil || !bytes.Equal(before, curr) {
		return nil, fmt.Errorf("persistent rules changed during reconciliation; retry")
	}
	if !bytes.Equal(before, after) {
		if e = store.Write(path, after, info.Mode().Perm()); e != nil {
			return nil, e
		}
	}
	if remove {
		return nil, os.Remove(ledger)
	}
	b, _ = json.Marshal(want)
	return want, store.Write(ledger, b, 0600)
}
