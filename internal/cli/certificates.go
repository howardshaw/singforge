package cli

import (
	"fmt"

	"github.com/howardshaw/singforge/internal/host"
	"github.com/howardshaw/singforge/internal/model"
)

func (op operation) certificateImport() error {
	s := op.state
	o := op.options
	name := o.get("host")
	if !model.Host(name) {
		return fmt.Errorf("invalid certificate hostname")
	}
	cert, e := read(o.get("cert"))
	if e != nil {
		return e
	}
	key, e := read(o.get("key"))
	if e != nil {
		return e
	}
	if e = host.CheckTrust(name, cert, key, ""); e != nil {
		return e
	}
	s.TLSName = name
	s.Certificate = cert
	s.Key = key
	return op.commit(s, "")
}
func (op operation) certificateRenew() error {
	s := op.state
	if !op.options.yes && !op.options.dry {
		return fmt.Errorf("client pins change; confirm with --yes")
	}
	v, e := model.New(s.Direct)
	if e != nil {
		return e
	}
	s.TLSName = ""
	s.Certificate = v.Certificate
	s.Key = v.Key
	return op.commit(s, "")
}
