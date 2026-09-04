//go:build cgo && !purego

// Package interop checks that this module's two µTP implementations agree on the wire.
//
// Every test runs over each ordered pair of implementations, so as well as purego against
// libutp in both roles, each is also tested against itself. libutp against libutp is the control:
// a failure there is the test's fault or the network's, not the port's.
//
// It's a separate package so that purego doesn't grow a dependency on cgo, and it needs libutp
// itself, so it's skipped when libutp isn't being compiled.
package interop

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/go-quicktest/qt"

	"github.com/anacrolix/go-libutp/utp"
)

// The implementations under test. Every test runs over each ordered pair of these, so as well as
// purego against libutp in both roles, each is run against itself.
var implementations = []utp.Implementation{utp.Libutp, utp.Purego}

const localhost = "localhost:0"

// Runs f once for each ordered pair of implementations.
func forEachPair(t *testing.T, f func(t *testing.T, dialer, acceptor utp.Implementation)) {
	t.Helper()
	for _, dialer := range implementations {
		for _, acceptor := range implementations {
			// Implementations print as their name.
			t.Run(fmt.Sprintf("%v_dials_%v", dialer, acceptor), func(t *testing.T) {
				f(t, dialer, acceptor)
			})
		}
	}
}

// Listens on a fresh loopback port.
func socket(t *testing.T, impl utp.Implementation) utp.Socket {
	t.Helper()
	s, err := utp.Listen(impl, "udp", localhost)
	qt.Assert(t, qt.IsNil(err), qt.Commentf("%v", impl))
	t.Cleanup(func() { s.Close() })
	return s
}

// Connects dialer to acceptor and returns both ends.
func connect(t *testing.T, dialer, acceptor utp.Socket) (dialed, accepted net.Conn) {
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
	qt.Assert(t, qt.IsNil(err))
	t.Cleanup(func() { dialed.Close() })
	r := <-accepts
	qt.Assert(t, qt.IsNil(r.err))
	t.Cleanup(func() { r.c.Close() })
	return dialed, r.c
}

// Connects one implementation to the other over ordinary loopback.
func connectPair(t *testing.T, dialer, acceptor utp.Implementation) (dialed, accepted net.Conn) {
	t.Helper()
	return connect(t, socket(t, dialer), socket(t, acceptor))
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	_, err := rand.Read(b)
	qt.Assert(t, qt.IsNil(err))
	return b
}

// Sends n random bytes from w to r and checks they arrive intact and in order. w is closed, which
// is what the reader sees as EOF.
func transfer(t *testing.T, w, r net.Conn, n int) {
	t.Helper()
	want := randomBytes(t, n)
	type result struct {
		b   []byte
		err error
	}
	reads := make(chan result, 1)
	go func() {
		b, err := io.ReadAll(r)
		reads <- result{b, err}
	}()
	qt.Assert(t, qt.IsNil(w.SetWriteDeadline(time.Now().Add(120*time.Second))))
	_, err := w.Write(want)
	qt.Assert(t, qt.IsNil(err))
	// Closing sends a FIN, which is what turns into EOF at the other end.
	qt.Assert(t, qt.IsNil(w.Close()))

	qt.Assert(t, qt.IsNil(r.SetReadDeadline(time.Now().Add(120*time.Second))))
	got := <-reads
	qt.Assert(t, qt.IsNil(got.err))
	qt.Assert(t, qt.Equals(len(got.b), len(want)))
	qt.Check(t, qt.IsTrue(bytes.Equal(want, got.b)), qt.Commentf("payload differs"))
}

// Sends n random bytes from w to r without closing either end, so both directions of a connection
// can be exercised at once.
func transferNoClose(t *testing.T, w, r net.Conn, n int) {
	t.Helper()
	want := randomBytes(t, n)
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
	qt.Assert(t, qt.IsNil(w.SetWriteDeadline(time.Now().Add(120*time.Second))))
	qt.Assert(t, qt.IsNil(r.SetReadDeadline(time.Now().Add(120*time.Second))))
	_, err := w.Write(want)
	qt.Assert(t, qt.IsNil(err))
	got := <-reads
	qt.Assert(t, qt.IsNil(got.err))
	qt.Check(t, qt.IsTrue(bytes.Equal(want, got.b)), qt.Commentf("payload differs"))
}

const transferSize = 1 << 20

// The dialling end sends.
func TestTransfer(t *testing.T) {
	forEachPair(t, func(t *testing.T, dialer, acceptor utp.Implementation) {
		dialed, accepted := connectPair(t, dialer, acceptor)
		transfer(t, dialed, accepted, transferSize)
	})
}

// The accepting end sends, which only works if the handshake completed without the dialer having
// anything of its own to say.
func TestReverseTransfer(t *testing.T) {
	forEachPair(t, func(t *testing.T, dialer, acceptor utp.Implementation) {
		dialed, accepted := connectPair(t, dialer, acceptor)
		transfer(t, accepted, dialed, transferSize)
	})
}

// Both directions at once, so acks ride along with data rather than arriving as state packets.
func TestBidirectional(t *testing.T) {
	forEachPair(t, func(t *testing.T, dialer, acceptor utp.Implementation) {
		dialed, accepted := connectPair(t, dialer, acceptor)
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

// A short exchange in both directions, which is the shape most protocols start with.
func TestPingPong(t *testing.T) {
	forEachPair(t, func(t *testing.T, dialer, acceptor utp.Implementation) {
		dialed, accepted := connectPair(t, dialer, acceptor)
		qt.Assert(t, qt.IsNil(dialed.SetDeadline(time.Now().Add(30*time.Second))))
		qt.Assert(t, qt.IsNil(accepted.SetDeadline(time.Now().Add(30*time.Second))))
		for i := 0; i < 20; i++ {
			_, err := io.WriteString(dialed, "ping")
			qt.Assert(t, qt.IsNil(err))
			b := make([]byte, 4)
			_, err = io.ReadFull(accepted, b)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(string(b), "ping"))

			_, err = io.WriteString(accepted, "pong")
			qt.Assert(t, qt.IsNil(err))
			_, err = io.ReadFull(dialed, b)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(string(b), "pong"))
		}
	})
}
