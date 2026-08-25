//go:build cgo && !purego

package interop

import (
	"math/rand/v2"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-quicktest/qt"

	"github.com/anacrolix/go-libutp/utp"
)

// How often, as a percentage, a packet is dropped outright and how often it's held back to arrive
// out of order. Enough to make both implementations work for it, low enough that libutp's sender,
// which backs off hard, still finishes quickly.
const (
	lossyDropPct  = 5
	lossyDelayPct = 5
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

// Listens on a fresh loopback port that mistreats the packets it sends. seed picks the pattern,
// so the two ends of a test misbehave differently but repeatably.
func lossySocket(t *testing.T, impl utp.Implementation, seed uint64) utp.Socket {
	t.Helper()
	pc, err := net.ListenPacket("udp", localhost)
	qt.Assert(t, qt.IsNil(err))
	s, err := impl.NewSocket(&lossyPacketConn{
		PacketConn: pc,
		r:          rand.New(rand.NewPCG(seed, seed*2+1)),
	})
	qt.Assert(t, qt.IsNil(err), qt.Commentf("%v", impl))
	t.Cleanup(func() { s.Close() })
	return s
}

// Both ends have to recover everything they send over a link that loses and reorders it,
// whichever implementation is at each end.
func TestLossyTransfer(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	forEachPair(t, func(t *testing.T, dialer, acceptor utp.Implementation) {
		dialed, accepted := connect(t, lossySocket(t, dialer, 1), lossySocket(t, acceptor, 2))
		// Deliberately modest: recovery under loss is what's being checked, and libutp's sender
		// backs off hard enough that a larger transfer only costs wall clock.
		transfer(t, dialed, accepted, 64<<10)
	})
}
