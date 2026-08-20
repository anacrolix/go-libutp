package interop

import (
	"bytes"
	cryptorand "crypto/rand"
	"io"
	"math/rand/v2"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-quicktest/qt"

	utp "github.com/anacrolix/go-libutp"
	"github.com/anacrolix/go-libutp/pureutp"
)

// Drops and delays outgoing packets, so the two implementations have to agree about
// retransmission, selective acks and reordering rather than just about the happy path.
type lossyPacketConn struct {
	net.PacketConn
	mu     sync.Mutex
	r      *rand.Rand
	wg     sync.WaitGroup
	closed bool
}

func (me *lossyPacketConn) roll() int {
	me.mu.Lock()
	defer me.mu.Unlock()
	return me.r.IntN(100)
}

// How often, as a percentage, a packet is dropped outright and how often it's held back to arrive
// out of order. Enough to make both implementations work for it, low enough that libutp's sender,
// which backs off hard, still finishes quickly.
const (
	lossyDropPct  = 5
	lossyDelayPct = 5
)

func (me *lossyPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if me.roll() < lossyDropPct {
		return len(b), nil
	}
	if me.roll() < lossyDelayPct {
		c := append([]byte(nil), b...)
		d := time.Duration(1+me.roll()%5) * time.Millisecond
		me.mu.Lock()
		if !me.closed {
			me.wg.Add(1)
			go func() {
				defer me.wg.Done()
				time.Sleep(d)
				me.PacketConn.WriteTo(c, addr)
			}()
		}
		me.mu.Unlock()
		return len(b), nil
	}
	return me.PacketConn.WriteTo(b, addr)
}

func (me *lossyPacketConn) Close() error {
	me.mu.Lock()
	me.closed = true
	me.mu.Unlock()
	err := me.PacketConn.Close()
	me.wg.Wait()
	return err
}

func newLossyPacketConn(t *testing.T, seed uint64) net.PacketConn {
	t.Helper()
	pc, err := net.ListenPacket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	return &lossyPacketConn{PacketConn: pc, r: rand.New(rand.NewPCG(seed, seed*2+1))}
}

func newLossyLibutpSocket(t *testing.T, seed uint64) socket {
	t.Helper()
	s, err := utp.NewSocketFromPacketConn(newLossyPacketConn(t, seed))
	qt.Assert(t, qt.IsNil(err))
	t.Cleanup(func() { s.Close() })
	return s
}

func newLossyPureSocket(t *testing.T, seed uint64) socket {
	t.Helper()
	s, err := pureutp.NewSocketFromPacketConn(newLossyPacketConn(t, seed))
	qt.Assert(t, qt.IsNil(err))
	t.Cleanup(func() { s.Close() })
	return s
}

func testLossyTransfer(t *testing.T, dialer, acceptor socket) {
	dialed, accepted := connect(t, dialer, acceptor)
	want := make([]byte, 128<<10)
	_, err := cryptorand.Read(want)
	qt.Assert(t, qt.IsNil(err))

	type result struct {
		b   []byte
		err error
	}
	reads := make(chan result, 1)
	go func() {
		b, err := io.ReadAll(accepted)
		reads <- result{b, err}
	}()
	qt.Assert(t, qt.IsNil(dialed.SetWriteDeadline(time.Now().Add(120*time.Second))))
	_, err = dialed.Write(want)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(dialed.Close()))
	qt.Assert(t, qt.IsNil(accepted.SetReadDeadline(time.Now().Add(120*time.Second))))
	got := <-reads
	qt.Assert(t, qt.IsNil(got.err))
	qt.Assert(t, qt.Equals(len(got.b), len(want)))
	qt.Check(t, qt.IsTrue(bytes.Equal(want, got.b)), qt.Commentf("payload differs"))
}

func TestLossyPureDialsLibutp(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	testLossyTransfer(t, newLossyPureSocket(t, 1), newLossyLibutpSocket(t, 2))
}

func TestLossyLibutpDialsPure(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	testLossyTransfer(t, newLossyLibutpSocket(t, 3), newLossyPureSocket(t, 4))
}
