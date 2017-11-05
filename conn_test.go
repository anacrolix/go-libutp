package utp

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnMethodsAfterClose(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	defer s.Close()
	d, a := connPairSocket(s)
	// We need to trigger libutp to destroy the Conns, the fastest way to do
	// this is destroy the parent Socket.
	assert.NoError(t, s.Close())
	for _, c := range []net.Conn{d, a} {
		// We're trying to test what happens when the Conn isn't known to
		// libutp anymore.
		assert.Nil(t, c.(*Conn).s)
		// These functions must not panic. I'm not sure we care what they
		// return.
		assert.NotPanics(t, func() { c.RemoteAddr() })
		assert.NotPanics(t, func() { c.LocalAddr() })
	}
}
