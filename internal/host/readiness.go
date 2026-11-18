package host

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/store"
)

type socket struct {
	transport string
	port      int
}

// Match the service process' file descriptors against its network namespace.
// An unrelated listener on the desired port must never satisfy readiness.
func processSockets(pid string) map[socket]bool {
	owned := map[string]bool{}
	out := map[socket]bool{}
	root := filepath.Join("/proc", pid)
	fds, _ := os.ReadDir(filepath.Join(root, "fd"))
	for _, f := range fds {
		target, e := os.Readlink(filepath.Join(root, "fd", f.Name()))
		if e == nil && strings.HasPrefix(target, "socket:[") {
			owned[strings.TrimSuffix(target[8:], "]")] = true
		}
	}
	for _, proto := range []string{"tcp", "udp"} {
		for _, suffix := range []string{"", "6"} {
			b, _ := os.ReadFile(filepath.Join(root, "net", proto+suffix))
			for line := range strings.SplitSeq(string(b), "\n") {
				v := strings.Fields(line)
				if len(v) < 10 || !owned[v[9]] || proto == "tcp" && v[3] != "0A" {
					continue
				}
				_, p, ok := strings.Cut(v[1], ":")
				if !ok {
					continue
				}
				n, e := strconv.ParseInt(p, 16, 32)
				if e == nil {
					out[socket{proto, int(n)}] = true
				}
			}
		}
	}
	return out
}
func wantedSockets(n string, s model.State) []socket {
	var expect []socket
	switch n {
	case subUnit:
		return []socket{{"tcp", 19781}}
	case edgeUnit:
		if s.CDN != "" {
			for _, ep := range s.Nodes() {
				if ep.Mode == "cdn" {
					expect = append(expect, socket{"tcp", s.CDNPort})
					break
				}
			}
		}
		if s.PublishHost != "" {
			expect = append(expect, socket{"tcp", 443})
		}
	case coreUnit:
		for _, a := range s.Inbounds {
			if !a.Enabled {
				continue
			}
			sp, _ := model.Lookup(a.Protocol)
			for _, proto := range sp.Networks() {
				expect = append(expect, socket{proto, a.Port})
			}
		}
	}
	return expect
}
func (m Manager) waitReady(units []string) error {
	if len(units) == 0 {
		return nil
	}
	s, e := store.Read(m.Dir)
	if e != nil {
		return e
	}
	for _, n := range units {
		expect := wantedSockets(n, s)
		if len(expect) == 0 {
			if e = run("systemctl", "is-active", "--quiet", n); e != nil {
				return fmt.Errorf("%s is inactive", n)
			}
			continue
		}
		deadline := time.Now().Add(25 * time.Second)
		for {
			b, e := exec.Command("systemctl", "show", "--property=MainPID", "--value", n).Output()
			pid := strings.TrimSpace(string(b))
			if id, err := strconv.Atoi(pid); e == nil && err == nil && id > 1 {
				got := processSockets(pid)
				ready := true
				for _, want := range expect {
					if !got[want] {
						ready = false
						break
					}
				}
				if ready {
					break
				}
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("%s failed to bind its configured sockets", n)
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	return nil
}

func (m Manager) Health() error {
	if !m.Live {
		return nil
	}
	var units []string
	for _, n := range all {
		if n == rulesService {
			continue
		}
		if _, e := os.Stat(filepath.Join(m.Dir, "current", n)); e == nil {
			units = append(units, n)
		}
	}
	if e := m.waitReady(units); e != nil {
		return e
	}
	return m.Service("status")
}

func (m Manager) ServiceStates() (map[string]string, error) {
	states := map[string]string{}
	for _, n := range all {
		if n == rulesService {
			continue
		}
		if _, e := os.Stat(filepath.Join(m.Dir, "current", n)); os.IsNotExist(e) {
			continue
		} else if e != nil {
			return nil, e
		}
		b, e := exec.Command("systemctl", "show", "-p", "ActiveState", "--value", n).Output()
		if e != nil {
			return nil, e
		}
		states[n] = strings.TrimSpace(string(b))
	}
	return states, nil
}
