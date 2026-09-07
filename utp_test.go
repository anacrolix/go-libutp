package utp

import (
	"context"
	"math"
	"net"
	"sync"
	"testing"
	"testing/quick"
	"time"

	_ "github.com/anacrolix/envpprof"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/nettest"
)

func doNettestTestConn(t *testing.T, swapConns bool, host string) {
	nettest.TestConn(t, func() (c1, c2 net.Conn, stop func(), err error) {
		s, err := NewSocket("inproc", net.JoinHostPort(host, "0"))
		if err != nil {
			return
		}
		c1, c2 = connPairSocket(s)
		if swapConns {
			c1, c2 = c2, c1
		}
		stop = func() {
			s.Close()
		}
		return
	})
}

func TestNettestTestConn(t *testing.T) {
	doNettestTestConn(t, false, "127.0.0.1")
}

func TestNettestTestConnIp6(t *testing.T) {
	doNettestTestConn(t, false, "::1")
}

func TestNettestTestConnSwapped(t *testing.T) {
	doNettestTestConn(t, true, "127.0.0.1")
}

func TestNettestTestConnSwappedIp6(t *testing.T) {
	doNettestTestConn(t, true, "::1")
}

func connPairSocket(s *Socket) (dialed net.Conn, accepted net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		var err error
		dialed, err = s.Dial(s.Addr().String())
		if err != nil {
			panic(err)
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		accepted, err = s.Accept()
		if err != nil {
			panic(err)
		}
	}()
	wg.Wait()
	return
}

const neverResponds = "localhost:1"

// Ensure that libutp dial timeouts out by itself.
func TestLibutpDialTimesOut(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.SkipNow()
	}
	s, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	defer s.Close()
	_, err = s.Dial(neverResponds)
	require.Error(t, err)
}

// Ensure that our timeout is honored during dialing.
func TestDialTimeout(t *testing.T) {
	t.Parallel()
	s, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	defer s.Close()
	const timeout = time.Second
	started := time.Now()
	_, err = s.DialTimeout(neverResponds, timeout)
	timeTaken := time.Since(started)
	t.Logf("dial returned after %s", timeTaken)
	assert.Equal(t, context.DeadlineExceeded, err)
	assert.True(t, timeTaken >= timeout)
}

func TestConnSendBuffer(t *testing.T) {
	s0, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	defer s0.Close()
	s1, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	defer s1.Close()
	var (
		c1        net.Conn
		acceptErr error
		accepted  = make(chan struct{})
	)
	go func() {
		defer close(accepted)
		c1, acceptErr = s1.Accept()
	}()
	c0, err := s0.Dial(s1.Addr().String())
	require.NoError(t, err)
	<-accepted
	require.NoError(t, acceptErr)
	defer c0.Close()
	defer c1.Close()
	buf := make([]byte, 1024)
	written := 0
	for {
		require.NoError(t, c0.SetWriteDeadline(time.Now().Add(time.Second)))
		n, err := c0.Write(buf)
		written += n
		if err != nil {
			t.Logf("stopped writing after error: %s", err)
			break
		}
	}
	t.Logf("write buffered %d bytes", written)
}

func TestCanHandleConnectWriteErrors(t *testing.T) {
	t.Parallel()
	s, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	defer s.Close()
	_, err = s.DialContext(context.Background(), "", "localhost:0")
	require.Error(t, err)
}

func TestConnectConnAfterSocketClose(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	s.Close()
	_, err = s.DialContext(context.Background(), "", "")
	require.Equal(t, errSocketClosed, err)
}

func assertSocketConnsLen(t *testing.T, s *Socket, l int) {
	mu.Lock()
	for len(s.conns) != l {
		s.logger.Info("waiting for conns to close", "socket", s, "conns", len(s.conns), "want", l)
		mu.Unlock()
		time.Sleep(time.Second)
		mu.Lock()
	}
	mu.Unlock()
}

func TestSocketConnsAfterConnClosed(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	defer s.Close()
	c, err := s.DialContext(context.Background(), "", s.LocalAddr().String())
	t.Logf("connecting to own socket: %v", err)
	if err == nil {
		c.Close()
		go func() {
			c, err := s.Accept()
			s.logger.Info("accepted", "err", err)
			c.Close()
		}()
	}
	assertSocketConnsLen(t, s, 0)
}

// Regression test for a lost-wakeup race between a read/write deadline
// timer firing and a goroutine that is about to call cond.Wait while
// holding mu. The deadline timer's callback must acquire mu before
// broadcasting: otherwise, if the timer fires exactly while another
// goroutine holds mu and is about to call cond.Wait (but hasn't yet), the
// broadcast happens before there's any registered waiter and is lost
// forever, leaving the waiter blocked even though its deadline has already
// expired.
func TestConnDeadlineTimerDoesNotLoseWakeup(t *testing.T) {
	c := &Conn{}
	c.cond.L = &mu
	c.writeDeadlineTimer = time.AfterFunc(time.Hour, c.broadcastCond)
	defer c.writeDeadlineTimer.Stop()

	// Mirrors how Conn.Write/Read hold mu continuously from before checking
	// the deadline until calling cond.Wait: lock mu here and don't release
	// it until Wait is called below, so the timer firing in between
	// exercises the exact race the fix guards against.
	mu.Lock()
	c.writeDeadlineTimer.Reset(time.Millisecond)
	// Give the timer's callback a chance to run and, without the fix, race
	// ahead of cond.Wait below by broadcasting before there's any waiter.
	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		// mu is already locked by this point (by the Lock call above); Wait
		// registers as a waiter and releases mu internally.
		c.cond.Wait()
		mu.Unlock()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadline timer's broadcast was lost; waiter never woke up")
	}
}

// Ensure that adding math.MaxInt64 to any current timestamp will result in the maximum "when" field
// for a Timer.
func TestMaxExpiryTimerMath(t *testing.T) {
	quick.Check(func(i int64) bool {
		i += math.MaxInt64
		return i == math.MaxInt64 || i < 0
	}, nil)
}
