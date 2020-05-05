package utp

import (
	"context"
	"sync"
	"testing"

	"github.com/bradfitz/iter"
	qt "github.com/frankban/quicktest"
)

// Test for a race that occurs if the error returned from PacketConn.WriteTo in sendtoCallback holds
// a reference to the addr passed to the call, and the addr storage is reused between calls to
// sendtoCallback in this instance.
func TestSendToRaceErrorAddr(t *testing.T) {
	c := qt.New(t)
	s, err := NewSocket("udp", "localhost:0")
	c.Assert(err, qt.IsNil)
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var wg sync.WaitGroup
	for range iter.N(2) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.DialContext(ctx, "udp", "1.1.1.1:1")
			c.Log(err.Error())
			c.Assert(err, qt.Not(qt.IsNil))
		}()
	}
	wg.Wait()
}
