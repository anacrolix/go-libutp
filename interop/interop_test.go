// Package interop checks that the pure Go implementation and libutp itself agree on the wire.
// It's a separate package so that the pure Go one doesn't grow a dependency on cgo.
package interop

import (
	"bytes"
	"crypto/rand"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	utp "github.com/anacrolix/go-libutp"
	"github.com/anacrolix/go-libutp/pureutp"
)

// Something that can dial and accept, so the two implementations can be swapped for each other.
type socket interface {
	Addr() net.Addr
	Accept() (net.Conn, error)
	Dial(addr string) (net.Conn, error)
	Close() error
}

func newLibutpSocket(t *testing.T) socket {
	t.Helper()
	s, err := utp.NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func newPureSocket(t *testing.T) socket {
	t.Helper()
	s, err := pureutp.NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

// Connects dialer to acceptor and returns both ends.
func connect(t *testing.T, dialer, acceptor socket) (dialed, accepted net.Conn) {
	t.Helper()
	type result struct {
		c   net.Conn
		err error
	}
	accepts := make(chan result, 1)
	go func() {
		c, err := acceptor.Accept()
		accepts <- result{c, err}
	}()
	dialed, err := dialer.Dial(acceptor.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { dialed.Close() })
	r := <-accepts
	require.NoError(t, r.err)
	t.Cleanup(func() { r.c.Close() })
	return dialed, r.c
}

// Sends n random bytes from w to r and checks they arrive intact and in order.
func transfer(t *testing.T, w, r net.Conn, n int) {
	t.Helper()
	want := make([]byte, n)
	_, err := rand.Read(want)
	require.NoError(t, err)

	type result struct {
		b   []byte
		err error
	}
	reads := make(chan result, 1)
	go func() {
		b, err := io.ReadAll(r)
		reads <- result{b, err}
	}()
	require.NoError(t, w.SetWriteDeadline(time.Now().Add(60*time.Second)))
	_, err = w.Write(want)
	require.NoError(t, err)
	// Closing sends a FIN, which is what turns into EOF at the other end.
	require.NoError(t, w.Close())

	require.NoError(t, r.SetReadDeadline(time.Now().Add(60*time.Second)))
	got := <-reads
	require.NoError(t, got.err)
	require.Equal(t, len(want), len(got.b))
	assert.True(t, bytes.Equal(want, got.b), "payload differs")
}

const transferSize = 1 << 20

func TestPureDialsLibutp(t *testing.T) {
	dialed, accepted := connect(t, newPureSocket(t), newLibutpSocket(t))
	transfer(t, dialed, accepted, transferSize)
}

func TestLibutpDialsPure(t *testing.T) {
	dialed, accepted := connect(t, newLibutpSocket(t), newPureSocket(t))
	transfer(t, dialed, accepted, transferSize)
}

// The accepting end sending first exercises the other direction of the handshake.
func TestPureDialsLibutpReverseTransfer(t *testing.T) {
	dialed, accepted := connect(t, newPureSocket(t), newLibutpSocket(t))
	transfer(t, accepted, dialed, transferSize)
}

func TestLibutpDialsPureReverseTransfer(t *testing.T) {
	dialed, accepted := connect(t, newLibutpSocket(t), newPureSocket(t))
	transfer(t, accepted, dialed, transferSize)
}

// Sends n random bytes from w to r without closing either end, so both directions of a connection
// can be exercised at once.
func transferNoClose(t *testing.T, w, r net.Conn, n int) {
	t.Helper()
	want := make([]byte, n)
	_, err := rand.Read(want)
	require.NoError(t, err)

	type result struct {
		b   []byte
		err error
	}
	reads := make(chan result, 1)
	go func() {
		b := make([]byte, n)
		_, err := io.ReadFull(r, b)
		reads <- result{b, err}
	}()
	require.NoError(t, w.SetWriteDeadline(time.Now().Add(60*time.Second)))
	require.NoError(t, r.SetReadDeadline(time.Now().Add(60*time.Second)))
	_, err = w.Write(want)
	require.NoError(t, err)
	got := <-reads
	require.NoError(t, got.err)
	assert.True(t, bytes.Equal(want, got.b), "payload differs")
}

// Both directions at once, so acks ride along with data rather than arriving as state packets.
func TestBidirectional(t *testing.T) {
	for _, c := range []struct {
		name             string
		dialer, acceptor func(*testing.T) socket
	}{
		{"PureDialsLibutp", newPureSocket, newLibutpSocket},
		{"LibutpDialsPure", newLibutpSocket, newPureSocket},
	} {
		t.Run(c.name, func(t *testing.T) {
			dialed, accepted := connect(t, c.dialer(t), c.acceptor(t))
			const n = 1 << 18
			done := make(chan struct{})
			go func() {
				defer close(done)
				transferNoClose(t, accepted, dialed, n)
			}()
			transferNoClose(t, dialed, accepted, n)
			<-done
		})
	}
}

// A short exchange in both directions, which is the shape most protocols start with.
func TestPingPong(t *testing.T) {
	for _, c := range []struct {
		name             string
		dialer, acceptor func(*testing.T) socket
	}{
		{"PureDialsLibutp", newPureSocket, newLibutpSocket},
		{"LibutpDialsPure", newLibutpSocket, newPureSocket},
	} {
		t.Run(c.name, func(t *testing.T) {
			dialed, accepted := connect(t, c.dialer(t), c.acceptor(t))
			require.NoError(t, dialed.SetDeadline(time.Now().Add(30*time.Second)))
			require.NoError(t, accepted.SetDeadline(time.Now().Add(30*time.Second)))
			for i := 0; i < 20; i++ {
				_, err := io.WriteString(dialed, "ping")
				require.NoError(t, err)
				b := make([]byte, 4)
				_, err = io.ReadFull(accepted, b)
				require.NoError(t, err)
				require.Equal(t, "ping", string(b))

				_, err = io.WriteString(accepted, "pong")
				require.NoError(t, err)
				_, err = io.ReadFull(dialed, b)
				require.NoError(t, err)
				require.Equal(t, "pong", string(b))
			}
		})
	}
}
