package purego

import (
	"testing"

	"github.com/go-quicktest/qt"
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
	qt.Check(t, qt.Equals(got, h))
}

// The type and version share a byte, high nibble first.
func TestHeaderTypeVersionPacking(t *testing.T) {
	var b [headerSize]byte
	h := header{Type: stSyn, Version: protocolVersion}
	h.marshalTo(b[:])
	qt.Check(t, qt.Equals(b[0], 0x41))
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
			qt.Check(t, qt.IsNotNil(err))
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
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(p.h.SeqNr, 7))
	qt.Check(t, qt.DeepEquals(p.selAck, []byte{0x0f, 0, 0, 0}))
	qt.Check(t, qt.Equals(string(p.payload), "abc"))
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
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(p.hasExtBits))
	qt.Check(t, qt.Equals(p.extBits, [8]byte{0, 1, 2, 3, 4, 5, 6, 7}))
	qt.Check(t, qt.DeepEquals(p.selAck, []byte{0xff, 0, 0, 0}))
	qt.Check(t, qt.Equals(string(p.payload), "hi"))
}

func TestStampHeader(t *testing.T) {
	var b [headerSize]byte
	(&header{Type: stState, Version: protocolVersion, SeqNr: 5, AckNr: 6}).marshalTo(b[:])
	stampHeader(b[:], 0xdeadbeef, 0xfeedface)
	setHeaderAckNr(b[:], 9)
	var got header
	got.unmarshal(b[:])
	qt.Check(t, qt.Equals(got.TimestampMicros, 0xdeadbeef))
	qt.Check(t, qt.Equals(got.TimestampDiffMicros, 0xfeedface))
	qt.Check(t, qt.Equals(got.AckNr, 9))
	// Stamping must leave the sequence number alone: a retransmission keeps the one it was
	// queued with.
	qt.Check(t, qt.Equals(headerSeqNr(b[:]), 5))
}

func TestWrappingCompareLess(t *testing.T) {
	const mask = 0xffff
	for _, c := range []struct {
		lhs, rhs uint32
		want     bool
	}{
		{1, 2, true},
		{2, 1, false},
		{1, 1, false},
		// Across the wrap, the shorter way round wins.
		{0xffff, 0, true},
		{0, 0xffff, false},
		{0xfffe, 3, true},
		{3, 0xfffe, false},
	} {
		qt.Check(t, qt.Equals(wrappingCompareLess(c.lhs, c.rhs, mask), c.want),
			qt.Commentf("%d < %d", c.lhs, c.rhs))
	}
}

// The delay history reports the smallest recent sample above the smallest sample ever seen.
func TestDelayHist(t *testing.T) {
	var h delayHist
	h.clear(0)
	// With no samples the history reads as zero delay, which is what libutp does too.
	qt.Check(t, qt.Equals(h.value(), 0), qt.Commentf("no samples"))
	h.addSample(1000, 0)
	qt.Check(t, qt.Equals(h.value(), 0), qt.Commentf("first sample becomes the base"))
	h.addSample(1500, 0)
	h.addSample(1800, 0)
	// Three samples fit in the history: 0, 500 and 800 above the base of 1000.
	qt.Check(t, qt.Equals(h.value(), 0), qt.Commentf("history full"))
	h.addSample(1600, 0)
	// 1000 has been pushed out, leaving 500, 800 and 600.
	qt.Check(t, qt.Equals(h.value(), 500), qt.Commentf("oldest sample rotated out"))
	// A lower sample moves the base, which lowers everything measured against it.
	h.addSample(900, 0)
	qt.Check(t, qt.Equals(h.value(), 0), qt.Commentf("base moved down"))
	qt.Check(t, qt.Equals(h.delayBase, 900))
}

func TestDelayHistShift(t *testing.T) {
	var h delayHist
	h.clear(0)
	h.addSample(1000, 0)
	h.shift(250)
	qt.Check(t, qt.Equals(h.delayBase, 1250))
}

func TestCircularBuffer(t *testing.T) {
	var b circularBuffer[int]
	b.init()
	qt.Check(t, qt.Equals(b.size(), 16))
	one, two := 1, 2
	b.put(5, &one)
	qt.Check(t, qt.Equals(b.get(5), &one))
	// Indexing wraps by the mask, which is what lets sequence numbers index it directly.
	qt.Check(t, qt.Equals(b.get(5+16), &one))
	b.put(6, &two)
	// Growing has to keep everything indexed by the same sequence numbers.
	b.ensureSize(20, 17)
	qt.Assert(t, qt.Equals(b.size(), 32))
	qt.Check(t, qt.Equals(b.get(5), &one))
	qt.Check(t, qt.Equals(b.get(6), &two))
	qt.Check(t, qt.IsNil(b.get(7)))
}

func TestReadBuffer(t *testing.T) {
	var b readBuffer
	b.write([]byte("hello "))
	b.write([]byte("world"))
	qt.Check(t, qt.Equals(b.len(), 11))
	p := make([]byte, 4)
	qt.Check(t, qt.Equals(b.read(p), 4))
	qt.Check(t, qt.Equals(string(p), "hell"))
	qt.Check(t, qt.Equals(b.len(), 7))
	// A read spanning what's left of one chunk and all of the next.
	p = make([]byte, 32)
	qt.Check(t, qt.Equals(b.read(p), 7))
	qt.Check(t, qt.Equals(string(p[:7]), "o world"))
	qt.Check(t, qt.Equals(b.len(), 0))
	qt.Check(t, qt.Equals(b.read(p), 0))
}

// A buffer written to must not alias the caller's slice, which is reused for the next read.
func TestReadBufferCopies(t *testing.T) {
	var b readBuffer
	p := []byte("abc")
	b.write(p)
	copy(p, "xyz")
	got := make([]byte, 3)
	b.read(got)
	qt.Check(t, qt.Equals(string(got), "abc"))
}
