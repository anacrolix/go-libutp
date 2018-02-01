package utp

import (
	"log"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"

	_ "github.com/anacrolix/envpprof"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/nettest"
)

func init() {
	log.SetFlags(log.Flags() | log.Lshortfile)
}

func doNettestTestConn(t *testing.T, swapConns bool) {
	nettest.TestConn(t, func() (c1, c2 net.Conn, stop func(), err error) {
		s, err := NewSocket("inproc", "localhost:0")
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
	t.Parallel()
	doNettestTestConn(t, false)
}

func TestNettestTestConnSwapped(t *testing.T) {
	t.Parallel()
	doNettestTestConn(t, true)
}

var rander = rand.New(rand.NewSource(time.Now().UnixNano()))

func connPairSocket(s *Socket) (dialed net.Conn, accepted net.Conn) {
	var (
		wg sync.WaitGroup
	)
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
	started := time.Now()
	_, err = s.DialTimeout(neverResponds, time.Second)
	require.Error(t, err)
	t.Log(err)
	assert.True(t, time.Now().Before(started.Add(2*time.Second)))
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
