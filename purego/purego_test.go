package purego

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/missinggo/inproc"
	"github.com/go-quicktest/qt"
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
	qt.Assert(t, qt.IsNil(dialErr))
	qt.Assert(t, qt.IsNil(acceptErr))
	return
}

func connPair(t *testing.T, network, host string) (dialed, accepted net.Conn, stop func()) {
	t.Helper()
	s, err := newTestSocket(network, host)
	qt.Assert(t, qt.IsNil(err))
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

	qt.Assert(t, qt.IsNil(c1.SetDeadline(time.Now().Add(10*time.Second))))
	qt.Assert(t, qt.IsNil(c2.SetDeadline(time.Now().Add(10*time.Second))))

	_, err := io.WriteString(c1, "hello uTP")
	qt.Assert(t, qt.IsNil(err))
	b := make([]byte, 9)
	_, err = io.ReadFull(c2, b)
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(string(b), "hello uTP"))

	_, err = io.WriteString(c2, "and back")
	qt.Assert(t, qt.IsNil(err))
	b = make([]byte, 8)
	_, err = io.ReadFull(c1, b)
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(string(b), "and back"))
}

// The accepting side has to be able to write first, which means the handshake must complete
// without the dialer sending anything of its own.
func TestAcceptedSideWritesFirst(t *testing.T) {
	c1, c2, stop := connPair(t, "udp", "localhost")
	defer stop()
	defer c1.Close()
	defer c2.Close()
	qt.Assert(t, qt.IsNil(c2.SetWriteDeadline(time.Now().Add(10*time.Second))))
	_, err := io.WriteString(c2, "server speaks first")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(c1.SetReadDeadline(time.Now().Add(10*time.Second))))
	b := make([]byte, 19)
	_, err = io.ReadFull(c1, b)
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(string(b), "server speaks first"))
}

func testTransfer(t *testing.T, network, host string, n int) {
	c1, c2, stop := connPair(t, network, host)
	defer stop()
	want := make([]byte, n)
	_, err := rand.Read(want)
	qt.Assert(t, qt.IsNil(err))

	var got []byte
	var readErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		got, readErr = io.ReadAll(c2)
	}()
	_, err = c1.Write(want)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(c1.Close()))
	<-done
	qt.Assert(t, qt.IsNil(readErr))
	qt.Assert(t, qt.Equals(len(got), len(want)))
	qt.Check(t, qt.IsTrue(bytes.Equal(want, got)), qt.Commentf("payload differs"))
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
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(c1.Close()))
	qt.Assert(t, qt.IsNil(c2.SetReadDeadline(time.Now().Add(10*time.Second))))
	b, err := io.ReadAll(c2)
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(string(b), "bye"))
}

func TestReadDeadline(t *testing.T) {
	c1, c2, stop := connPair(t, "udp", "localhost")
	defer stop()
	defer c1.Close()
	defer c2.Close()
	qt.Assert(t, qt.IsNil(c2.SetReadDeadline(time.Now().Add(50*time.Millisecond))))
	_, err := c2.Read(make([]byte, 1))
	var nerr net.Error
	qt.Assert(t, qt.ErrorAs(err, &nerr))
	qt.Check(t, qt.IsTrue(nerr.Timeout()))
	// Clearing the deadline lets reads work again.
	qt.Assert(t, qt.IsNil(c2.SetReadDeadline(time.Time{})))
	_, err = io.WriteString(c1, "x")
	qt.Assert(t, qt.IsNil(err))
	b := make([]byte, 1)
	_, err = io.ReadFull(c2, b)
	qt.Assert(t, qt.IsNil(err))
}

func TestUseClosedConn(t *testing.T) {
	c1, c2, stop := connPair(t, "udp", "localhost")
	defer stop()
	defer c2.Close()
	qt.Assert(t, qt.IsNil(c1.Close()))
	_, err := c1.Write([]byte("x"))
	qt.Check(t, qt.ErrorIs(err, net.ErrClosed))
	_, err = c1.Read(make([]byte, 1))
	qt.Check(t, qt.ErrorIs(err, net.ErrClosed))
}

const neverResponds = "localhost:1"

func TestDialContextTimeout(t *testing.T) {
	t.Parallel()
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	const timeout = 500 * time.Millisecond
	started := time.Now()
	_, err = s.DialTimeout(neverResponds, timeout)
	qt.Check(t, qt.ErrorIs(err, context.DeadlineExceeded))
	elapsed := time.Since(started)
	qt.Check(t, qt.IsTrue(elapsed >= timeout), qt.Commentf("dial gave up after %v, want at least %v", elapsed, timeout))
}

// With nothing at the other end, the connection attempt gives up by itself.
func TestDialTimesOutByItself(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.SkipNow()
	}
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	_, err = s.Dial(neverResponds)
	qt.Check(t, qt.ErrorIs(err, ErrTimedOut))
}

func TestUseClosedSocket(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(s.Close()))
	// Closing twice must be harmless rather than panicking.
	qt.Check(t, qt.IsNil(s.Close()))
	c, err := s.Dial(neverResponds)
	qt.Check(t, qt.ErrorIs(err, ErrSocketClosed))
	qt.Check(t, qt.IsNil(c))
	_, err = s.Accept()
	qt.Check(t, qt.ErrorIs(err, ErrSocketClosed))
}

func TestSocketNetwork(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	qt.Check(t, qt.Equals(s.Addr().Network(), "udp"))
}

// A Socket passes datagrams that aren't uTP through to ReadFrom, so the port can be shared.
func TestNonUtpPassthrough(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	other, err := net.ListenPacket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer other.Close()

	_, err = other.WriteTo([]byte("not uTP at all"), s.Addr())
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(s.SetReadDeadline(time.Now().Add(10*time.Second))))
	b := make([]byte, 64)
	n, from, err := s.ReadFrom(b)
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(string(b[:n]), "not uTP at all"))
	qt.Check(t, qt.Equals(from.String(), other.LocalAddr().String()))
}

func TestSocketReadFromDeadline(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	qt.Assert(t, qt.IsNil(s.SetReadDeadline(time.Now().Add(50*time.Millisecond))))
	_, _, err = s.ReadFrom(make([]byte, 1))
	var nerr net.Error
	qt.Assert(t, qt.ErrorAs(err, &nerr))
	qt.Check(t, qt.IsTrue(nerr.Timeout()))
}

// Connections have to be forgotten once they're closed at both ends, or a long lived Socket
// leaks them.
func TestConnsReapedAfterClose(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	c1, c2 := connPairSocket(t, s)
	qt.Assert(t, qt.IsNil(c1.Close()))
	qt.Assert(t, qt.IsNil(c2.Close()))
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
