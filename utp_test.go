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

func TestNettestLocalhostUDP(t *testing.T) {
	t.Parallel()
	nettest.TestConn(t, func() (c1, c2 net.Conn, stop func(), err error) {
		s, err := NewSocket("udp", "localhost:0")
		if err != nil {
			return
		}
		c1, c2 = connPairSocket(s)
		stop = func() {
			s.Close()
		}
		return
	})
}

var rander = rand.New(rand.NewSource(time.Now().UnixNano()))

func connPairSocket(s *Socket) (net.Conn, net.Conn) {
	var (
		wg               sync.WaitGroup
		dialed, accepted net.Conn
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
	switch rander.Intn(2) {
	case 0:
		log.Printf("first of conn pair is dialed")
		return dialed, accepted
	case 1:
		log.Print("first of conn pair is accepted")
		return accepted, dialed
	default:
		panic("ಠ_ಠ")
	}
}

const neverResponds = "localhost:1"

// Ensure that libutp dial timeouts out by itself.
func TestLibutpDialTimesOut(t *testing.T) {
	t.Parallel()
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
