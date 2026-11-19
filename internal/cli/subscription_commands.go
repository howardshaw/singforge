package cli

import (
	"fmt"
	"time"

	"github.com/howardshaw/singforge/internal/model"
	"github.com/howardshaw/singforge/internal/render"
	"github.com/howardshaw/singforge/internal/subscription"
)

func (op operation) subscriptionShow() error {
	o := op.options
	s := op.state
	return export(s, o)
}
func (op operation) subscriptionList() error {
	s := op.state
	return printJSON(map[string]any{"host": s.PublishHost, "grants": len(s.Grants)})
}
func (op operation) subscriptionRotate() error {
	o := op.options
	s := op.state
	if o.ttl < 0 || o.grace < 0 {
		return fmt.Errorf("negative duration")
	}
	if !model.Domain(s.PublishHost) {
		return fmt.Errorf("HTTPS publication is off; select Enable HTTPS publication or run singforge subscription publish --help")
	}
	if _, _, e := render.Export(s, o.get("format"), ""); e != nil {
		return e
	}
	var token string
	if op.path == "subscription issue" {
		token = subscription.Issue(&s, time.Now(), o.ttl)
	} else {
		token = subscription.Rotate(&s, time.Now(), o.ttl, o.grace)
	}
	secret := "https://" + s.PublishHost + "/s/" + token + "/" + o.get("format")
	return op.commit(s, secret)
}
func (op operation) subscriptionRevoke() error {
	o := op.options
	s := op.state
	cmd := op.path
	if !o.yes && !o.dry {
		return fmt.Errorf("confirm with --yes")
	}
	s.Grants = nil
	if cmd == "subscription unpublish" {
		s.AutoRenew = false
		s.PublishHost = ""
		s.PublishCert = ""
		s.PublishKey = ""
	}
	return op.commit(s, "")
}
func (op operation) subscriptionPublish() error {
	o := op.options
	s := op.state
	var e error
	s.AutoRenew = false
	s.PublishHost = o.get("host")
	s.PublishCA = ""
	if p := o.get("ca"); p != "" {
		s.PublishCA, e = read(p)
		if e != nil {
			return e
		}
	}
	if !model.Domain(s.PublishHost) {
		return fmt.Errorf("provide --host FQDN")
	}
	s.PublishCert, e = read(o.get("cert"))
	if e == nil {
		s.PublishKey, e = read(o.get("key"))
	}
	if e != nil {
		return e
	}
	return op.commit(s, "")
}

func (op operation) subscriptionNodes() error {
	ids := []string{}
	for _, n := range op.state.Nodes() {
		if _, _, e := render.Export(op.state, "uri", n.ID); e == nil {
			ids = append(ids, n.ID)
		}
	}
	return printJSON(ids)
}
