package utp

import "testing"

func TestInit(t *testing.T) {
	s := NewSocket()
	s.Accept()
}
