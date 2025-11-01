package iox

var ZeroReader zeroReader

type zeroReader struct{}

func (me zeroReader) Read(b []byte) (n int, err error) {
	for i := range b {
		b[i] = 0
	}
	n = len(b)
	return
}
