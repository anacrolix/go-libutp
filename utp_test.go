package utp

import (
	"log"
	"net"
	"sync"
	"testing"

	_ "github.com/anacrolix/envpprof"

	"golang.org/x/net/nettest"
)

func init() {
	log.SetFlags(log.Flags() | log.Lshortfile)
}

func TestNettestLocalhostUDP(t *testing.T) {
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

func connPairSocket(s *Socket) (initer, accepted net.Conn) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var err error
		initer, err = s.Dial(s.Addr().String())
		if err != nil {
			panic(err)
		}
	}()
	accepted, err := s.Accept()
	if err != nil {
		panic(err)
	}
	wg.Wait()
	return
}
