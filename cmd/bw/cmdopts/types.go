package cmdopts

import (
	"net"
	"reflect"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/davecgh/go-spew/spew"
	"github.com/pkg/errors"
)

// ParseIP addresses
func ParseIP(ctx *kong.DecodeContext, target reflect.Value) (err error) {
	target.Set(reflect.ValueOf(net.ParseIP(ctx.Scan.Pop().String())))
	return nil
}

func ParseTCPAddr(ctx *kong.DecodeContext, target reflect.Value) (err error) {
	if ctx.Scan.Len() == 0 {
		return nil
	}

	var (
		saddr = ctx.Scan.Pop().String()
	)

	var (
		addr *net.TCPAddr
	)

	if addr, err = net.ResolveTCPAddr("tcp", saddr); err != nil {
		return errors.Wrapf(err, "unable to resolve tcp address %s - %s", saddr, spew.Sdump(ctx))
	}

	target.Set(reflect.ValueOf(addr))

	return nil
}

func ParseTCPAddrArray(ctx *kong.DecodeContext, target reflect.Value) (err error) {

	if ctx.Scan.Len() == 0 {
		return nil
	}

	var (
		results []*net.TCPAddr
		token   = ctx.Scan.Pop().String()
	)

	token = strings.ReplaceAll(token, "\n", " ")
	token = strings.ReplaceAll(token, ",", " ")
	for _, saddr := range strings.Split(token, " ") {
		var (
			addr *net.TCPAddr
		)

		if addr, err = net.ResolveTCPAddr("tcp", saddr); err != nil {
			return errors.Wrapf(err, "unable to resolve tcp address %s : %s", saddr, token)
		}

		results = append(results, addr)
	}

	target.Set(reflect.ValueOf(results))
	return nil
}
