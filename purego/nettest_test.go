package purego

import (
	"net"
	"testing"

	"golang.org/x/net/nettest"
)

// nettest.TestConn is the standard conformance suite for net.Conn: ordinary IO, ping pong,
// deadlines in the past, present and future, concurrent use of every method, and reads and writes
// racing against deadline changes.
func doNettestTestConn(t *testing.T, swapConns bool, network, host string) {
	nettest.TestConn(t, func() (c1, c2 net.Conn, stop func(), err error) {
		s, err := newTestSocket(network, host)
		if err != nil {
			return
		}
		var wg [2]net.Conn
		errs := make(chan error, 2)
		go func() {
			var err error
			wg[0], err = s.Dial(s.Addr().String())
			errs <- err
		}()
		go func() {
			var err error
			wg[1], err = s.Accept()
			errs <- err
		}()
		for range wg {
			if err = <-errs; err != nil {
				s.Close()
				return
			}
		}
		c1, c2 = wg[0], wg[1]
		if swapConns {
			c1, c2 = c2, c1
		}
		stop = func() {
			c1.Close()
			c2.Close()
			s.Close()
		}
		return
	})
}

func TestNettestTestConn(t *testing.T) {
	doNettestTestConn(t, false, "inproc", "")
}

func TestNettestTestConnSwapped(t *testing.T) {
	doNettestTestConn(t, true, "inproc", "")
}

func TestNettestTestConnUDP(t *testing.T) {
	doNettestTestConn(t, false, "udp", "127.0.0.1")
}

func TestNettestTestConnUDPSwapped(t *testing.T) {
	doNettestTestConn(t, true, "udp", "127.0.0.1")
}

func TestNettestTestConnIPv6(t *testing.T) {
	requireIPv6(t)
	doNettestTestConn(t, false, "udp", "::1")
}
