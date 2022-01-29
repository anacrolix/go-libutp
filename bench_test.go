package utp

import (
	"io"
	"io/ioutil"
	"net"
	"testing"

	"github.com/anacrolix/log"
	"github.com/anacrolix/missinggo"
	"github.com/bradfitz/iter"

	"github.com/stretchr/testify/require"
)

func benchmarkThroughput(t *testing.B, n int64) {
	s1, err := NewSocket("udp", "localhost:0", log.Logger{})
	require.NoError(t, err)
	defer s1.Close()
	s2, err := NewSocket("udp", "localhost:0", log.Logger{})
	require.NoError(t, err)
	defer s2.Close()
	var c2 net.Conn
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		var err error
		c2, err = s2.Accept()
		require.NoError(t, err)
	}()
	c1, err := s1.Dial(s2.Addr().String())
	require.NoError(t, err)
	defer c1.Close()
	<-accepted
	defer c2.Close()
	t.SetBytes(n)
	t.ReportAllocs()
	for range iter.N(t.N) {
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

func BenchmarkThroughput100MB(t *testing.B) {
	benchmarkThroughput(t, 100<<20)
}

func BenchmarkThroughput10MB(t *testing.B) {
	benchmarkThroughput(t, 10<<20)
}

func BenchmarkThroughput1MB(t *testing.B) {
	benchmarkThroughput(t, 1<<20)
}
