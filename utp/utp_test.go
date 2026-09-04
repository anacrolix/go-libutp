package utp

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-quicktest/qt"
)

// Dials a Socket from itself, which is the cheapest way to get a connected pair.
func connPair(t *testing.T, s Socket) (dialed, accepted net.Conn) {
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
	t.Cleanup(func() {
		dialed.Close()
		accepted.Close()
	})
	return
}

// Whichever implementation the build selected has to carry a stream intact.
func TestSelectedImplementationTransfers(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	c1, c2 := connPair(t, s)

	want := make([]byte, 1<<19)
	_, err = rand.Read(want)
	qt.Assert(t, qt.IsNil(err))
	reads := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(c2)
		reads <- b
	}()
	qt.Assert(t, qt.IsNil(c1.SetWriteDeadline(time.Now().Add(60*time.Second))))
	_, err = c1.Write(want)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(c1.Close()))
	got := <-reads
	qt.Assert(t, qt.Equals(len(got), len(want)))
	qt.Check(t, qt.IsTrue(bytes.Equal(want, got)), qt.Commentf("payload differs"))
}

// A Socket is a listener and a packet conn whichever implementation is underneath, and the
// connections it hands out honour deadlines.
func TestInterfacesSatisfied(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	var _ net.Listener = s
	var _ net.PacketConn = s
	qt.Check(t, qt.Equals(s.Addr().Network(), "udp"))
	qt.Check(t, qt.Equals(s.LocalAddr().String(), s.Addr().String()))

	c1, _ := connPair(t, s)
	qt.Assert(t, qt.IsNil(c1.SetReadDeadline(time.Now().Add(50*time.Millisecond))))
	_, err = c1.Read(make([]byte, 1))
	var nerr net.Error
	qt.Assert(t, qt.ErrorAs(err, &nerr))
	qt.Check(t, qt.IsTrue(nerr.Timeout()))
}

// Datagrams that aren't µTP reach ReadFrom, so the port can be shared.
func TestNonUtpPassthrough(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	other, err := net.ListenPacket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer other.Close()
	_, err = other.WriteTo([]byte("not uTP at all"), s.Addr())
	qt.Assert(t, qt.IsNil(err))

	// libutp's Socket has no deadlines, so read on a goroutine and give up by other means.
	type read struct {
		b    []byte
		from net.Addr
	}
	reads := make(chan read, 1)
	go func() {
		b := make([]byte, 64)
		n, from, err := s.ReadFrom(b)
		if err == nil {
			reads <- read{b[:n], from}
		}
	}()
	select {
	case got := <-reads:
		qt.Check(t, qt.Equals(string(got.b), "not uTP at all"))
		qt.Check(t, qt.Equals(got.from.String(), other.LocalAddr().String()))
	case <-time.After(30 * time.Second):
		t.Fatal("non-uTP datagram never arrived")
	}
}

func TestBufferSizeOptions(t *testing.T) {
	const send, receive = 128 << 10, 96 << 10
	s, err := NewSocket("udp", "localhost:0", WithBufferSizes(send, receive))
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	qt.Check(t, qt.Equals(s.WriteBufferLen(), send))
	qt.Check(t, qt.Equals(s.ReadBufferLen(), receive))
	s.SetWriteBufferLen(send * 2)
	s.SetReadBufferLen(receive * 2)
	qt.Check(t, qt.Equals(s.WriteBufferLen(), send*2))
	qt.Check(t, qt.Equals(s.ReadBufferLen(), receive*2))
}

// A firewall callback that rejects everything means the peer gets no answer at all, so the dial
// times out rather than being refused.
func TestFirewallCallback(t *testing.T) {
	acceptor, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer acceptor.Close()
	var mu sync.Mutex
	var asked int
	acceptor.SetFirewallCallback(func(net.Addr) bool {
		mu.Lock()
		asked++
		mu.Unlock()
		return true
	})
	dialer, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer dialer.Close()

	_, err = dialer.DialTimeout(acceptor.Addr().String(), 2*time.Second)
	qt.Check(t, qt.IsNotNil(err))
	mu.Lock()
	defer mu.Unlock()
	qt.Check(t, qt.IsTrue(asked > 0), qt.Commentf("firewall callback was never consulted"))
}

// Purego is available whatever the build selected, and an Implementation is a value that can be
// passed around and used without knowing which one it is.
func TestPuregoAlwaysAvailable(t *testing.T) {
	var impl Implementation = Purego
	qt.Check(t, qt.Equals(fmt.Sprint(impl), "purego"))
	s, err := Listen(impl, "udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	qt.Check(t, qt.IsNil(s.SetReadDeadline(time.Now().Add(time.Hour))))
}

// An Implementation only has to make a Socket over a PacketConn. Listen is what opens one for it,
// and it doesn't leave the port open if the Socket can't be built.
func TestListen(t *testing.T) {
	s, err := Listen(Default, "udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	qt.Check(t, qt.Equals(s.Addr().Network(), "udp"))

	_, err = Listen(Default, "tcp", "localhost:0")
	qt.Check(t, qt.IsNotNil(err), qt.Commentf("listened on a network that isn't packet oriented"))
}

// NewSocket is Default.NewSocket, and NewSocketFromPacketConn takes a PacketConn the caller
// already owns.
func TestDefaultAndPackageFunctions(t *testing.T) {
	qt.Assert(t, qt.IsNotNil(Default))
	name := fmt.Sprint(Default)
	qt.Check(t, qt.IsTrue(name == "libutp" || name == "purego"),
		qt.Commentf("unexpected implementation %q", name))

	pc, err := net.ListenPacket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	s, err := NewSocketFromPacketConn(pc)
	qt.Assert(t, qt.IsNil(err))
	// The Socket owns the PacketConn now, including its port.
	qt.Check(t, qt.Equals(s.Addr().String(), pc.LocalAddr().String()))
	qt.Assert(t, qt.IsNil(s.Close()))
}

// Turning the protocol log categories on and off works on either implementation. There's nothing
// to observe without capturing the logger, so this is a smoke test that neither panics or
// rejects it.
func TestSetLogging(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	s.SetLogging(true, true, true)
	s.SetLogging(false, false, false)
}

// Collects everything logged to it, so a test can see what a Socket had to say.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (me *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (me *captureHandler) Handle(_ context.Context, r slog.Record) error {
	me.mu.Lock()
	defer me.mu.Unlock()
	me.records = append(me.records, r.Clone())
	return nil
}

func (me *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return me }
func (me *captureHandler) WithGroup(string) slog.Handler      { return me }

func (me *captureHandler) len() int {
	me.mu.Lock()
	defer me.mu.Unlock()
	return len(me.records)
}

// A PacketConn that can't send, so that a Socket over it has something to report.
type unsendablePacketConn struct {
	net.PacketConn
}

func (me unsendablePacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	return 0, errors.New("this conn doesn't send")
}

// What a Socket logs goes to the logger WithLogger gave it, on either implementation.
func TestWithLogger(t *testing.T) {
	h := new(captureHandler)
	pc, err := net.ListenPacket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	s, err := NewSocketFromPacketConn(unsendablePacketConn{pc}, WithLogger(slog.New(h)))
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	// purego reports a failed send under its normal category; libutp reports it regardless.
	s.SetLogging(true, true, true)
	// The dial's syn is a send, and every send on this Socket fails, so the dial can only time
	// out. It's what it logs on the way there that this is about.
	_, err = s.DialTimeout(s.Addr().String(), time.Second)
	qt.Check(t, qt.IsNotNil(err))
	qt.Check(t, qt.IsTrue(h.len() > 0), qt.Commentf("nothing reached the logger"))
}
