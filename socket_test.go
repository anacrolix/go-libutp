package utp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUseClosedSocket(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	assert.NoError(t, s.Close())
	assert.NotPanics(t, func() { s.Close() })
	c, err := s.Dial(neverResponds)
	assert.Error(t, err)
	assert.Nil(t, c)
}
