package utp

import (
	"net"
	"testing"

	"github.com/go-quicktest/qt"
)

func TestConnMethodsAfterClose(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	d, a := connPairSocket(s)
	// We need to trigger libutp to destroy the Conns, the fastest way to do
	// this is destroy the parent Socket.
	qt.Check(t, qt.IsNil(s.Close()))
	for _, c := range []net.Conn{d, a} {
		// We're trying to test what happens when the Conn isn't known to
		// libutp anymore.
		qt.Check(t, qt.IsNil(c.(*Conn).us))
		// These functions must not panic. I'm not sure we care what they
		// return.
		qt.Check(t, qt.Not(qt.PanicMatches(func() { c.RemoteAddr() }, ".*")))
		qt.Check(t, qt.Not(qt.PanicMatches(func() { c.LocalAddr() }, ".*")))
	}
}
