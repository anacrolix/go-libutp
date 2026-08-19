package pureutp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeaderRoundTrip(t *testing.T) {
	h := header{
		Type:                stData,
		Version:             protocolVersion,
		Extension:           extNone,
		ConnID:              0xbeef,
		TimestampMicros:     0x11223344,
		TimestampDiffMicros: 0x55667788,
		WndSize:             1 << 20,
		SeqNr:               0x1234,
		AckNr:               0x4321,
	}
	var b [headerSize]byte
	h.marshalTo(b[:])
	var got header
	got.unmarshal(b[:])
	assert.Equal(t, h, got)
}

// The type and version share a byte, high nibble first.
func TestHeaderTypeVersionPacking(t *testing.T) {
	var b [headerSize]byte
	h := header{Type: stSyn, Version: protocolVersion}
	h.marshalTo(b[:])
	assert.EqualValues(t, 0x41, b[0])
}

func TestParseRejectsNonUtp(t *testing.T) {
	for _, c := range []struct {
		name string
		b    []byte
	}{
		{"empty", nil},
		{"short", make([]byte, headerSize-1)},
		{"bad type", func() []byte {
			b := make([]byte, headerSize)
			b[0] = 0x51 // type 5, one past the last valid one
			return b
		}()},
		{"version 0", func() []byte {
			b := make([]byte, headerSize)
			b[0] = 0x40
			return b
		}()},
		{"implausible extension", func() []byte {
			b := make([]byte, headerSize)
			b[0] = 0x41
			b[1] = 3
			return b
		}()},
		{"extension runs past the end", func() []byte {
			b := make([]byte, headerSize+2)
			b[0] = 0x41
			b[1] = extSelectiveAck
			b[headerSize] = extNone
			b[headerSize+1] = 4 // claims four bytes that aren't there
			return b
		}()},
		{"truncated extension header", func() []byte {
			b := make([]byte, headerSize+1)
			b[0] = 0x41
			b[1] = extSelectiveAck
			return b
		}()},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := parsePacket(c.b)
			assert.Error(t, err)
		})
	}
}

func TestParseSelectiveAckAndPayload(t *testing.T) {
	b := make([]byte, headerSize+6+3)
	h := header{Type: stData, Version: protocolVersion, Extension: extSelectiveAck, SeqNr: 7}
	h.marshalTo(b)
	b[headerSize] = extNone
	b[headerSize+1] = 4
	b[headerSize+2] = 0x0f
	copy(b[headerSize+6:], "abc")
	p, err := parsePacket(b)
	require.NoError(t, err)
	assert.EqualValues(t, 7, p.h.SeqNr)
	assert.Equal(t, []byte{0x0f, 0, 0, 0}, p.selAck)
	assert.Equal(t, "abc", string(p.payload))
}

// A chain of extensions has to be walked to find the payload, and unknown types skipped.
func TestParseExtensionChain(t *testing.T) {
	b := make([]byte, headerSize+10+6+2)
	h := header{Type: stSyn, Version: protocolVersion, Extension: extBits}
	h.marshalTo(b)
	b[headerSize] = extSelectiveAck
	b[headerSize+1] = 8
	for i := 0; i < 8; i++ {
		b[headerSize+2+i] = byte(i)
	}
	b[headerSize+10] = extNone
	b[headerSize+11] = 4
	b[headerSize+12] = 0xff
	copy(b[headerSize+16:], "hi")
	p, err := parsePacket(b)
	require.NoError(t, err)
	require.True(t, p.hasExtBits)
	assert.Equal(t, [8]byte{0, 1, 2, 3, 4, 5, 6, 7}, p.extBits)
	assert.Equal(t, []byte{0xff, 0, 0, 0}, p.selAck)
	assert.Equal(t, "hi", string(p.payload))
}

func TestStampHeader(t *testing.T) {
	var b [headerSize]byte
	(&header{Type: stState, Version: protocolVersion, SeqNr: 5, AckNr: 6}).marshalTo(b[:])
	stampHeader(b[:], 0xdeadbeef, 0xfeedface)
	setHeaderAckNr(b[:], 9)
	var got header
	got.unmarshal(b[:])
	assert.EqualValues(t, 0xdeadbeef, got.TimestampMicros)
	assert.EqualValues(t, 0xfeedface, got.TimestampDiffMicros)
	assert.EqualValues(t, 9, got.AckNr)
	assert.EqualValues(t, 5, headerSeqNr(b[:]))
}

func TestWrappingCompareLess(t *testing.T) {
	const mask = 0xffff
	assert.True(t, wrappingCompareLess(1, 2, mask))
	assert.False(t, wrappingCompareLess(2, 1, mask))
	assert.False(t, wrappingCompareLess(1, 1, mask))
	// Across the wrap, the shorter way round wins.
	assert.True(t, wrappingCompareLess(0xffff, 0, mask))
	assert.False(t, wrappingCompareLess(0, 0xffff, mask))
	assert.True(t, wrappingCompareLess(0xfffe, 3, mask))
}

// The delay history reports the smallest recent sample above the smallest sample ever seen.
func TestDelayHist(t *testing.T) {
	var h delayHist
	h.clear(0)
	// With no samples the history reads as zero delay, which is what libutp does too.
	assert.EqualValues(t, 0, h.value())
	h.addSample(1000, 0)
	assert.EqualValues(t, 0, h.value())
	h.addSample(1500, 0)
	h.addSample(1800, 0)
	// Three samples fit in the history: 0, 500, 800 above the base of 1000.
	assert.EqualValues(t, 0, h.value())
	h.addSample(1600, 0)
	// 1000 has been pushed out, leaving 500, 800 and 600.
	assert.EqualValues(t, 500, h.value())
	// A lower sample moves the base, which lowers everything measured against it.
	h.addSample(900, 0)
	assert.EqualValues(t, 0, h.value())
	assert.EqualValues(t, 900, h.delayBase)
}

func TestDelayHistShift(t *testing.T) {
	var h delayHist
	h.clear(0)
	h.addSample(1000, 0)
	h.shift(250)
	assert.EqualValues(t, 1250, h.delayBase)
}

func TestCircularBuffer(t *testing.T) {
	var b circularBuffer[int]
	b.init()
	assert.EqualValues(t, 16, b.size())
	one, two := 1, 2
	b.put(5, &one)
	assert.Equal(t, &one, b.get(5))
	// Indexing wraps by the mask, which is what lets sequence numbers index it directly.
	assert.Equal(t, &one, b.get(5+16))
	b.put(6, &two)
	// Growing has to keep everything indexed by the same sequence numbers.
	b.ensureSize(20, 17)
	assert.EqualValues(t, 32, b.size())
	assert.Equal(t, &one, b.get(5))
	assert.Equal(t, &two, b.get(6))
	assert.Nil(t, b.get(7))
}

func TestReadBuffer(t *testing.T) {
	var b readBuffer
	b.write([]byte("hello "))
	b.write([]byte("world"))
	assert.Equal(t, 11, b.len())
	p := make([]byte, 4)
	assert.Equal(t, 4, b.read(p))
	assert.Equal(t, "hell", string(p))
	assert.Equal(t, 7, b.len())
	p = make([]byte, 32)
	assert.Equal(t, 7, b.read(p))
	assert.Equal(t, "o world", string(p[:7]))
	assert.Equal(t, 0, b.len())
	assert.Equal(t, 0, b.read(p))
}

// A buffer written to must not alias the caller's slice, which is reused for the next read.
func TestReadBufferCopies(t *testing.T) {
	var b readBuffer
	p := []byte("abc")
	b.write(p)
	copy(p, "xyz")
	got := make([]byte, 3)
	b.read(got)
	assert.Equal(t, "abc", string(got))
}
