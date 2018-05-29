package utp

import (
	"io"
	"io/ioutil"
	"net"
	"testing"

	"github.com/anacrolix/missinggo"
	"github.com/bradfitz/iter"

	"github.com/stretchr/testify/require"
)

func BenchmarkThroughput(t *testing.B) {
	s1, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	defer s1.Close()
	s2, err := NewSocket("udp", "localhost:0")
	require.NoError(t, err)
	defer s2.Close()
	var c2 net.Conn
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		c2, err = s2.Accept()
		require.NoError(t, err)
	}()
	c1, err := s1.Dial(s2.Addr().String())
	require.NoError(t, err)
	defer c1.Close()
	<-accepted
	defer c2.Close()
	var n int64 = 100 << 20
	t.SetBytes(n)
	for range iter.N(t.N) {
		// log.Print(i)
		doneReading := make(chan struct{})
		go func() {
			defer close(doneReading)
			wn, err := io.CopyN(ioutil.Discard, c2, n)
			require.NoError(t, err)
			require.EqualValues(t, n, wn)
		}()
		wn, err := io.CopyN(c1, missinggo.ZeroReader, n)
		require.NoError(t, err)
		require.EqualValues(t, n, wn)
		<-doneReading
	}
}
