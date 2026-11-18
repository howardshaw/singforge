package cli

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"time"

	"github.com/howardshaw/singforge/internal/model"
)

func (op operation) protocolList() error {
	return printJSON(model.Catalog)
}
func (op operation) protocolEnable() error {
	o := op.options
	s := op.state
	var e error
	sp, ok := model.Lookup(o.get("id"))
	if !ok {
		return fmt.Errorf("unknown protocol")
	}
	if sp.WS {
		_, e = s.Enable(sp.ID)
		if e == nil && s.CDN == "" && s.Tunnel == "" {
			return fmt.Errorf("enable a CDN or tunnel first")
		}
		for _, x := range []struct {
			mode, h string
			p       int
		}{{"cdn", s.CDN, s.CDNPort}, {"tunnel", s.Tunnel, 443}} {
			if e == nil && x.h != "" {
				e = s.Add(sp.ID, x.mode, x.h, x.p)
			}
		}
	} else {
		e = Direct(&s, sp.ID, o.port)
	}
	if e != nil {
		return e
	}
	return op.commit(s, "")
}
func (op operation) protocolConfigure() error {
	o := op.options
	s := op.state
	cmd := op.path
	found := false
	for i := range s.Inbounds {
		a := &s.Inbounds[i]
		if a.Protocol == o.get("id") {
			found = true
			if cmd == "protocol disable" {
				a.Enabled = false
			} else {
				a.Port = o.port
				for j := range s.Endpoints {
					ep := &s.Endpoints[j]
					if ep.Inbound == a.Protocol && ep.Mode == "direct" {
						ep.Port = o.port
					}
				}
			}
		}
	}
	if !found {
		return fmt.Errorf("unknown installed protocol")
	}
	return op.commit(s, "")
}
func (op operation) endpointAdd() error {
	o := op.options
	s := op.state
	var e error
	id := o.get("id")
	a, e := s.Enable(id)
	if e != nil {
		return e
	}
	port := o.port
	if port == 0 {
		port = a.Port
		if o.get("mode") == "cdn" {
			port = s.CDNPort
		}
		if o.get("mode") == "tunnel" {
			port = 443
		}
	}
	if name := o.get("name"); name != "" {
		tmp := model.State{}
		if err := tmp.Add(id, o.get("mode"), o.get("host"), port); err != nil {
			return err
		}
		ep := tmp.Endpoints[0]
		ep.ID = name
		s.Endpoints = append(s.Endpoints, ep)
	} else {
		e = s.Add(id, o.get("mode"), o.get("host"), port)
	}
	if e != nil {
		return e
	}
	if e != nil {
		return e
	}
	return op.commit(s, "")
}
func (op operation) endpointRemove() error {
	o := op.options
	s := op.state
	if !o.yes && !o.dry {
		return fmt.Errorf("confirm with --yes")
	}
	count := len(s.Endpoints)
	s.Endpoints = slices.DeleteFunc(s.Endpoints, func(ep model.Endpoint) bool { return ep.ID == o.get("id") })
	if len(s.Endpoints) == count {
		return fmt.Errorf("unknown endpoint")
	}
	return op.commit(s, "")
}
func (op operation) endpointList() error {
	s := op.state
	return printJSON(s.Endpoints)
}
func (op operation) endpointTest() error {
	o := op.options
	s := op.state
	for _, n := range s.Nodes() {
		if n.ID == o.get("id") {
			if n.Spec.ID == "tuic" || n.Spec.ID == "hysteria2" {
				return fmt.Errorf("UDP requires an authenticated client traffic test")
			}
			c, e := net.DialTimeout("tcp", net.JoinHostPort(n.Host, strconv.Itoa(n.Port)), 5*time.Second)
			if e != nil {
				return e
			}
			c.Close()
			fmt.Println("TCP reachable")
			return nil
		}
	}
	return fmt.Errorf("unknown enabled endpoint")
}

func (op operation) realityConfigure() error {
	s := op.state
	s.RealitySNI = op.options.get("handshake")
	s.RealityTarget = op.options.get("handshake-target")
	return op.commit(s, "")
}
