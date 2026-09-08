package utp

import (
	"context"
	"sync"
	"testing"

	"github.com/bradfitz/iter"
	"github.com/go-quicktest/qt"
)

// Test for a race that occurs if the error returned from PacketConn.WriteTo in sendtoCallback holds
// a reference to the addr passed to the call, and the addr storage is reused between calls to
// sendtoCallback in this instance.
func TestSendToRaceErrorAddr(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var wg sync.WaitGroup
	for range iter.N(2) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.DialContext(ctx, "udp", "1.1.1.1:1")
			t.Log(err.Error())
			qt.Assert(t, qt.Not(qt.IsNil(err)))
		}()
	}
	wg.Wait()
}
