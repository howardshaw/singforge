package cli

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"time"

	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/render"
	"github.com/howardshaw/singforge/internal/routing"
)

func (op operation) dnsSet() error {
	o := op.options
	s := op.state
	if o.get("profile") != "" {
		s.DNS = o.get("profile")
	}
	if o.get("resolver") != "" {
		s.Resolver = o.get("resolver")
	}
	if v := o.get("server-family"); v != "" {
		s.ServerDNSFamily = v
	}
	if v := o.get("capture"); v != "" {
		s.Capture = v
	}
	if v := o.get("family"); v != "" {
		s.DNSFamily = v
	}
	if v := o.get("transport"); v != "" {
		s.DNSTransport = v
	}
	return op.commit(s, "")
}
func (op operation) dnsShow() error {
	s := op.state
	return printJSON(map[string]string{"profile": s.DNS, "resolver": s.Resolver, "transport": s.DNSTransport, "capture": s.Capture, "family": s.DNSFamily})
}
func (op operation) rulesList() error {
	s := op.state
	return printJSON(map[string]any{"builtin": render.Smart, "version": s.Rules.Version, "cached_rules": len(s.Rules.Rules), "overrides": s.Overrides})
}
func (op operation) rulesValidate() error {
	s := op.state
	return s.Validate()
}
func (op operation) rulesUpdate() error {
	o := op.options
	s := op.state
	if o.dry {
		return printJSON(routing.Sources)
	}
	snap, err := routing.Update()
	if err != nil {
		return err
	}
	if snap.Version == s.Rules.Version {
		return nil
	}
	s.PreviousRules = s.Rules
	s.Rules = snap
	return op.commit(s, "")
}
func (op operation) rulesRollback() error {
	o := op.options
	s := op.state
	if !o.yes {
		return fmt.Errorf("confirm with --yes")
	}
	s.Rules, s.PreviousRules = s.PreviousRules, s.Rules
	return op.commit(s, "")
}
func (op operation) rulesOverride() error {
	o := op.options
	s := op.state
	r := routing.Rule{Kind: o.get("kind"), Value: o.get("value"), Group: o.get("group")}
	if e := r.Check(); e != nil {
		return e
	}
	s.Overrides = append([]routing.Rule{r}, s.Overrides...)
	return op.commit(s, "")
}
func (op operation) rulesTest() error {
	o := op.options
	s := op.state
	fmt.Println(render.Explain(s, o.get("host")))
	return nil
}
func (op operation) rulesSet() error {
	o := op.options
	s := op.state
	s.Routing = o.get("profile")
	if s.Routing == "gfw-only" && s.Rules.Version == "" {
		return fmt.Errorf("download GFW rules with rules update first")
	}
	if !slices.Contains([]string{"smart", "global", "gfw-only", "custom"}, s.Routing) {
		return fmt.Errorf("unsupported routing profile")
	}
	return op.commit(s, "")
}

func (op operation) dnsTest() error {
	h := op.options.get("host")
	if !model.Domain(h) {
		return fmt.Errorf("provide a DNS hostname")
	}
	p := op.options.port
	if p == 0 {
		p = 1053
	}
	if p < 1 || p > 65535 {
		return fmt.Errorf("invalid resolver port")
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(p))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resolver := net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}}
	ips, e := resolver.LookupIPAddr(ctx, h)
	if e != nil {
		return fmt.Errorf("local DNS query failed; run the exported client and check its DNS listener")
	}
	return printJSON(map[string]any{"queried": addr, "host": h, "answers": ips})
}

func (op operation) rulesSchedule() error {
	if !op.options.yes && !op.options.dry {
		return fmt.Errorf("confirm timer change with --yes")
	}
	s := op.state
	s.ScheduleRules = op.path == "rules schedule"
	return op.commit(s, "")
}
