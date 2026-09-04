package purego

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
)

// lossyPacketConn drops, duplicates and delays outgoing packets, so that retransmission, the
// reorder buffer and selective acks all get exercised. Loopback on its own delivers everything in
// order and none of that code would ever run.
type lossyPacketConn struct {
	net.PacketConn
	mu sync.Mutex
	r  *rand.Rand
	// Probabilities out of 100.
	dropPct, dupPct, delayPct int
	wg                        sync.WaitGroup
	closed                    bool
}

func newLossyPacketConn(pc net.PacketConn, seed uint64, dropPct, dupPct, delayPct int) *lossyPacketConn {
	return &lossyPacketConn{
		PacketConn: pc,
		r:          rand.New(rand.NewPCG(seed, seed*2+1)),
		dropPct:    dropPct,
		dupPct:     dupPct,
		delayPct:   delayPct,
	}
}

func (me *lossyPacketConn) roll() int {
	me.mu.Lock()
	defer me.mu.Unlock()
	return me.r.IntN(100)
}

func (me *lossyPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if me.roll() < me.dropPct {
		return len(b), nil
	}
	send := func() (int, error) { return me.PacketConn.WriteTo(b, addr) }
	if me.roll() < me.delayPct {
		// Hold the packet back so it arrives after ones sent later.
		c := append([]byte(nil), b...)
		me.mu.Lock()
		if !me.closed {
			me.wg.Add(1)
			go func() {
				defer me.wg.Done()
				time.Sleep(time.Duration(1+me.roll()%5) * time.Millisecond)
				me.PacketConn.WriteTo(c, addr)
			}()
		}
		me.mu.Unlock()
		return len(b), nil
	}
	n, err := send()
	if err == nil && me.roll() < me.dupPct {
		me.PacketConn.WriteTo(b, addr)
	}
	return n, err
}

func (me *lossyPacketConn) Close() error {
	me.mu.Lock()
	me.closed = true
	me.mu.Unlock()
	err := me.PacketConn.Close()
	me.wg.Wait()
	return err
}

func TestTransferOverLossyNetwork(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	pc1, err := net.ListenPacket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	pc2, err := net.ListenPacket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	s1, err := NewSocketFromPacketConn(newLossyPacketConn(pc1, 1, 5, 2, 5))
	qt.Assert(t, qt.IsNil(err))
	defer s1.Close()
	s2, err := NewSocketFromPacketConn(newLossyPacketConn(pc2, 2, 5, 2, 5))
	qt.Assert(t, qt.IsNil(err))
	defer s2.Close()

	accepts := make(chan net.Conn, 1)
	go func() {
		c, err := s2.Accept()
		if err != nil {
			close(accepts)
			return
		}
		accepts <- c
	}()
	c1, err := s1.Dial(s2.Addr().String())
	qt.Assert(t, qt.IsNil(err))
	defer c1.Close()
	c2, ok := <-accepts
	qt.Assert(t, qt.IsTrue(ok))
	defer c2.Close()

	want := make([]byte, 256<<10)
	_, err = cryptorand.Read(want)
	qt.Assert(t, qt.IsNil(err))

	type result struct {
		b   []byte
		err error
	}
	reads := make(chan result, 1)
	go func() {
		b, err := io.ReadAll(c2)
		reads <- result{b, err}
	}()
	qt.Assert(t, qt.IsNil(c1.SetWriteDeadline(time.Now().Add(120*time.Second))))
	_, err = c1.Write(want)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(c1.Close()))
	qt.Assert(t, qt.IsNil(c2.SetReadDeadline(time.Now().Add(120*time.Second))))
	got := <-reads
	qt.Assert(t, qt.IsNil(got.err))
	qt.Assert(t, qt.Equals(len(got.b), len(want)))
	qt.Check(t, qt.IsTrue(bytes.Equal(want, got.b)), qt.Commentf("payload differs"))
}

// A lost syn-ack has to be recoverable: the peer asks again with another SYN, and the answer has
// to come back, or an accepted connection is stranded until the dialer gives up.
func TestSynAckLostThenRecovered(t *testing.T) {
	acceptorPc, err := net.ListenPacket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	// Drop the first two packets the acceptor sends, which are its first two syn-acks.
	gated := &dropFirstN{PacketConn: acceptorPc, n: 2}
	acceptor, err := NewSocketFromPacketConn(gated)
	qt.Assert(t, qt.IsNil(err))
	defer acceptor.Close()
	dialer, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer dialer.Close()

	accepts := make(chan net.Conn, 1)
	go func() {
		c, err := acceptor.Accept()
		if err != nil {
			close(accepts)
			return
		}
		accepts <- c
	}()
	c1, err := dialer.DialTimeout(acceptor.Addr().String(), 30*time.Second)
	qt.Assert(t, qt.IsNil(err))
	defer c1.Close()
	c2, ok := <-accepts
	qt.Assert(t, qt.IsTrue(ok))
	defer c2.Close()

	qt.Assert(t, qt.IsNil(c1.SetDeadline(time.Now().Add(30*time.Second))))
	qt.Assert(t, qt.IsNil(c2.SetDeadline(time.Now().Add(30*time.Second))))
	_, err = io.WriteString(c1, "made it")
	qt.Assert(t, qt.IsNil(err))
	b := make([]byte, 7)
	_, err = io.ReadFull(c2, b)
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(string(b), "made it"))
}

type dropFirstN struct {
	net.PacketConn
	mu sync.Mutex
	n  int
}

func (me *dropFirstN) WriteTo(b []byte, addr net.Addr) (int, error) {
	me.mu.Lock()
	drop := me.n > 0
	if drop {
		me.n--
	}
	me.mu.Unlock()
	if drop {
		return len(b), nil
	}
	return me.PacketConn.WriteTo(b, addr)
}
