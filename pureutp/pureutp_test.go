package pureutp

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/missinggo/inproc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Dials a Socket from itself, which is how the nettest suite gets a connected pair.
func connPairSocket(t *testing.T, s *Socket) (dialed, accepted net.Conn) {
	t.Helper()
	var wg sync.WaitGroup
	var dialErr, acceptErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		dialed, dialErr = s.Dial(s.Addr().String())
	}()
	go func() {
		defer wg.Done()
		accepted, acceptErr = s.Accept()
	}()
	wg.Wait()
	require.NoError(t, dialErr)
	require.NoError(t, acceptErr)
	return
}

func connPair(t *testing.T, network, host string) (dialed, accepted net.Conn, stop func()) {
	t.Helper()
	s, err := newTestSocket(network, host)
	require.NoError(t, err)
	dialed, accepted = connPairSocket(t, s)
	return dialed, accepted, func() { s.Close() }
}

// Listens on a real UDP port, or on the in-process packet network the tests use to run without
// touching the machine's network stack.
func newTestSocket(network, host string) (*Socket, error) {
	if network == "inproc" {
		pc, err := inproc.ListenPacket(network, "")
		if err != nil {
			return nil, err
		}
		return NewSocketFromPacketConn(pc, WithAddrResolver(inproc.ResolveAddr))
	}
	return NewSocket(network, net.JoinHostPort(host, "0"))
}

func TestDialAcceptEcho(t *testing.T) {
	c1, c2, stop := connPair(t, "udp", "localhost")
	defer stop()
	defer c1.Close()
	defer c2.Close()

	require.NoError(t, c1.SetDeadline(time.Now().Add(10*time.Second)))
	require.NoError(t, c2.SetDeadline(time.Now().Add(10*time.Second)))

	_, err := io.WriteString(c1, "hello uTP")
	require.NoError(t, err)
	b := make([]byte, 9)
	_, err = io.ReadFull(c2, b)
	require.NoError(t, err)
	assert.Equal(t, "hello uTP", string(b))

	_, err = io.WriteString(c2, "and back")
	require.NoError(t, err)
	b = make([]byte, 8)
	_, err = io.ReadFull(c1, b)
	require.NoError(t, err)
	assert.Equal(t, "and back", string(b))
}

// The accepting side has to be able to write first, which means the handshake must complete
// without the dialer sending anything of its own.
func TestAcceptedSideWritesFirst(t *testing.T) {
	c1, c2, stop := connPair(t, "udp", "localhost")
	defer stop()
	defer c1.Close()
	defer c2.Close()
	require.NoError(t, c2.SetWriteDeadline(time.Now().Add(10*time.Second)))
	_, err := io.WriteString(c2, "server speaks first")
	require.NoError(t, err)
	require.NoError(t, c1.SetReadDeadline(time.Now().Add(10*time.Second)))
	b := make([]byte, 19)
	_, err = io.ReadFull(c1, b)
	require.NoError(t, err)
	assert.Equal(t, "server speaks first", string(b))
}

func testTransfer(t *testing.T, network, host string, n int) {
	c1, c2, stop := connPair(t, network, host)
	defer stop()
	want := make([]byte, n)
	_, err := rand.Read(want)
	require.NoError(t, err)

	var got []byte
	var readErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		got, readErr = io.ReadAll(c2)
	}()
	_, err = c1.Write(want)
	require.NoError(t, err)
	require.NoError(t, c1.Close())
	<-done
	require.NoError(t, readErr)
	require.Equal(t, len(want), len(got))
	assert.True(t, bytes.Equal(want, got))
	c2.Close()
}

func TestTransferUDP(t *testing.T) {
	testTransfer(t, "udp", "localhost", 1<<20)
}

func TestTransferIPv6(t *testing.T) {
	requireIPv6(t)
	testTransfer(t, "udp", "::1", 1<<18)
}

func requireIPv6(t *testing.T) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 here: %v", err)
	}
	pc.Close()
}

func TestTransferInproc(t *testing.T) {
	testTransfer(t, "inproc", "", 1<<18)
}

// Closing one end has to show up as EOF at the other.
func TestCloseGivesEOF(t *testing.T) {
	c1, c2, stop := connPair(t, "udp", "localhost")
	defer stop()
	defer c2.Close()
	_, err := io.WriteString(c1, "bye")
	require.NoError(t, err)
	require.NoError(t, c1.Close())
	require.NoError(t, c2.SetReadDeadline(time.Now().Add(10*time.Second)))
	b, err := io.ReadAll(c2)
	require.NoError(t, err)
	assert.Equal(t, "bye", string(b))
}

func TestReadDeadline(t *testing.T) {
	c1, c2, stop := connPair(t, "udp", "localhost")
	defer stop()
	defer c1.Close()
	defer c2.Close()
	require.NoError(t, c2.SetReadDeadline(time.Now().Add(50*time.Millisecond)))
	_, err := c2.Read(make([]byte, 1))
	var nerr net.Error
	require.True(t, errors.As(err, &nerr), "%v", err)
	assert.True(t, nerr.Timeout())
	// Clearing the deadline lets reads work again.
	require.NoError(t, c2.SetReadDeadline(time.Time{}))
	_, err = io.WriteString(c1, "x")
	require.NoError(t, err)
	b := make([]byte, 1)
	_, err = io.ReadFull(c2, b)
	require.NoError(t, err)
}

func TestUseClosedConn(t *testing.T) {
	c1, c2, stop := connPair(t, "udp", "localhost")
	defer stop()
	defer c2.Close()
	require.NoError(t, c1.Close())
	_, err := c1.Write([]byte("x"))
	assert.ErrorIs(t, err, net.ErrClosed)
	_, err = c1.Read(make([]byte, 1))
	assert.ErrorIs(t, err, net.ErrClosed)
}

const neverResponds = "localhost:1"

func TestDialContextTimeout(t *testing.T) {
	t.Parallel()
	s, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	defer s.Close()
	const timeout = 500 * time.Millisecond
	started := time.Now()
	_, err = s.DialTimeout(neverResponds, timeout)
	assert.Equal(t, context.DeadlineExceeded, err)
	assert.GreaterOrEqual(t, time.Since(started), timeout)
}

// With nothing at the other end, the connection attempt gives up by itself.
func TestDialTimesOutByItself(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.SkipNow()
	}
	s, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	defer s.Close()
	_, err = s.Dial(neverResponds)
	require.ErrorIs(t, err, ErrTimedOut)
}

func TestUseClosedSocket(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	require.NoError(t, s.Close())
	assert.NotPanics(t, func() { s.Close() })
	c, err := s.Dial(neverResponds)
	assert.ErrorIs(t, err, ErrSocketClosed)
	assert.Nil(t, c)
	_, err = s.Accept()
	assert.ErrorIs(t, err, ErrSocketClosed)
}

func TestSocketNetwork(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	defer s.Close()
	assert.Equal(t, "udp", s.Addr().Network())
}

// A Socket passes datagrams that aren't uTP through to ReadFrom, so the port can be shared.
func TestNonUtpPassthrough(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	defer s.Close()
	other, err := net.ListenPacket("udp", "localhost:0")
	require.NoError(t, err)
	defer other.Close()

	_, err = other.WriteTo([]byte("not uTP at all"), s.Addr())
	require.NoError(t, err)
	require.NoError(t, s.SetReadDeadline(time.Now().Add(10*time.Second)))
	b := make([]byte, 64)
	n, from, err := s.ReadFrom(b)
	require.NoError(t, err)
	assert.Equal(t, "not uTP at all", string(b[:n]))
	assert.Equal(t, other.LocalAddr().String(), from.String())
}

func TestSocketReadFromDeadline(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.SetReadDeadline(time.Now().Add(50*time.Millisecond)))
	_, _, err = s.ReadFrom(make([]byte, 1))
	var nerr net.Error
	require.True(t, errors.As(err, &nerr), "%v", err)
	assert.True(t, nerr.Timeout())
}

// Connections have to be forgotten once they're closed at both ends, or a long lived Socket
// leaks them.
func TestConnsReapedAfterClose(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	defer s.Close()
	c1, c2 := connPairSocket(t, s)
	require.NoError(t, c1.Close())
	require.NoError(t, c2.Close())
	deadline := time.Now().Add(30 * time.Second)
	for {
		s.mu.Lock()
		n := len(s.conns)
		s.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d conns still registered", n)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
