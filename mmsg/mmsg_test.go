package mmsg

import (
	"math/rand"
	"net"
	"testing"

	// testify sux, switch to frankban/quicktest
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func udpSocket(t *testing.T) interface {
	net.PacketConn
	net.Conn
} {
	pc, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 0,
	})
	require.NoError(t, err)
	return pc
}

func payload(t *testing.T) string {
	n := rand.Intn(512) + 1
	b := make([]byte, n)
	nn, err := rand.Read(b)
	require.NoError(t, err)
	assert.Equal(t, n, nn)
	return string(b)
}

func TestReceiveBatch(t *testing.T) {
	s := udpSocket(t)
	r := udpSocket(t)
	mc := NewConn(r)
	b1 := payload(t)
	b2 := payload(t)
	s.WriteTo([]byte(b1), r.LocalAddr())
	s.WriteTo([]byte(b2), r.LocalAddr())
	var ms []Message
	for range 3 {
		ms = append(ms, Message{
			Buffers: [][]byte{make([]byte, 0x1000)},
		})
	}
	n, err := mc.RecvMsgs(ms)
	assert.NoError(t, err)
	t.Log(n)
	if mc.Err() == nil {
		assert.Equal(t, 2, n)
		assert.EqualValues(t, b1, ms[0].Payload())
		assert.EqualValues(t, b2, ms[1].Payload())
	} else {
		t.Logf("error using multi: %s", mc.Err())
		assert.Equal(t, 1, n)
		assert.EqualValues(t, b1, ms[0].Payload())
	}
}
