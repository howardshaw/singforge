package host

import (
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"

	"github.com/howardshaw/singforge/internal/model"
)

func probePort(network, host string, port int) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	var listener io.Closer
	var err error
	if network == "udp" {
		listener, err = net.ListenPacket(network, addr)
	} else {
		listener, err = net.Listen(network, addr)
	}
	if err != nil {
		return fmt.Errorf("cannot bind %s %s: %w", network, addr, err)
	}
	return listener.Close()
}
func unitSockets(n string) map[socket]bool {
	v, e := exec.Command("systemctl", "show", "-p", "MainPID", "--value", n).Output()
	pid := strings.TrimSpace(string(v))
	if id, err := strconv.Atoi(pid); e == nil && err == nil && id > 1 {
		return processSockets(pid)
	}
	return nil
}
func preflightPorts(s model.State) error {
	owned := unitSockets(coreUnit)
	for _, a := range s.Inbounds {
		if !a.Enabled {
			continue
		}
		sp, _ := model.Lookup(a.Protocol)
		addr := s.Listen
		if sp.WS {
			addr = "127.0.0.1"
		}
		for _, n := range sp.Networks() {
			if !owned[socket{n, a.Port}] {
				if e := probePort(n, addr, a.Port); e != nil {
					return e
				}
			}
		}
	}
	for _, unit := range []string{edgeUnit, subUnit, tunnelUnit} {
		addr := "127.0.0.1"
		var need []socket
		switch unit {
		case edgeUnit:
			addr = ""
			need = wantedSockets(unit, s)
		case subUnit:
			if s.PublishHost != "" {
				need = wantedSockets(unit, s)
			}
		case tunnelUnit:
			if s.Tunnel != "" {
				need = []socket{{"tcp", 19782}}
			}
		}
		if len(need) == 0 {
			continue
		}
		existing := unitSockets(unit)
		for _, p := range need {
			if !existing[p] {
				if e := probePort(p.transport, addr, p.port); e != nil {
					return e
				}
			}
		}
	}
	return nil
}
