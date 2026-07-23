package utp

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// gatedPacketConn only lets packets be received when a token is available. It
// lets tests control exactly when the wrapped Socket processes what it's been
// sent. Closing tokens lets everything through.
type gatedPacketConn struct {
	net.PacketConn
	tokens chan struct{}
}

func (me gatedPacketConn) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	<-me.tokens
	n, addr, err = me.PacketConn.ReadFrom(b)
	return
}

// An incoming connection isn't established in libutp until the initiator's
// first data packet arrives. Until then utp_write refuses to write, and Conn
// writers block waiting for the connection to become writable. Completing the
// connection has to wake them up.
func TestWriteOnAcceptedConnBeforeEstablished(t *testing.T) {
	dialerPc, err := listenPacket("inproc", "")
	require.NoError(t, err)
	acceptorPc, err := listenPacket("inproc", "")
	require.NoError(t, err)
	// Only the initiating SYN gets through until we release the gate.
	tokens := make(chan struct{}, 1)
	tokens <- struct{}{}

	dialer, err := NewSocketFromPacketConn(dialerPc)
	require.NoError(t, err)
	defer dialer.Close()
	acceptor, err := NewSocketFromPacketConn(gatedPacketConn{acceptorPc, tokens})
	require.NoError(t, err)
	defer acceptor.Close()

	dialed := make(chan net.Conn, 1)
	go func() {
		c, err := dialer.Dial(acceptor.Addr().String())
		if err != nil {
			t.Errorf("dialing: %v", err)
			close(dialed)
			return
		}
		dialed <- c
	}()

	accepted, err := acceptor.Accept()
	require.NoError(t, err)
	defer accepted.Close()

	// The acceptor hasn't processed the dialer's data packet yet, so libutp
	// still considers the connection to be handshaking and the write blocks.
	written := make(chan error, 1)
	go func() {
		_, err := accepted.Write([]byte("hello"))
		written <- err
	}()
	select {
	case err := <-written:
		t.Fatalf("write completed before the connection was established: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	// Let the acceptor see the dialer's data packet. That establishes the
	// connection, which must wake the blocked write.
	close(tokens)
	select {
	case err := <-written:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("write didn't complete after the connection was established")
	}

	if c := <-dialed; c != nil {
		c.Close()
	}
}
