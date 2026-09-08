package utp

import (
	"testing"

	"github.com/go-quicktest/qt"
)

func TestUseClosedSocket(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.IsNil(s.Close()))
	qt.Check(t, qt.Not(qt.PanicMatches(func() { s.Close() }, ".*")))
	c, err := s.Dial(neverResponds)
	qt.Check(t, qt.IsNotNil(err))
	qt.Check(t, qt.IsNil(c))
}

func TestSocketNetwork(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	qt.Check(t, qt.Equals(s.Addr().Network(), "udp"))
}
