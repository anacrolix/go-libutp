package utp

/*
#include "utp.h"
*/
import "C"
import (
	"errors"
	"io"
	"net"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type Conn struct {
	s          *C.utp_socket
	cond       sync.Cond
	readBuf    []byte
	gotEOF     bool
	gotConnect bool
	// Set on state changed to UTP_STATE_DESTROYING. Not valid to refer to the
	// socket after getting this.
	destroyed bool
	// Conn.Close was called.
	closed bool

	writeDeadline time.Time
	readDeadline  time.Time
}

func (c *Conn) setConnected() {
	c.gotConnect = true
	c.cond.Broadcast()
}

func (c *Conn) waitForConnect() error {
	for !c.gotConnect {
		c.cond.Wait()
	}
	return nil
}

func (c *Conn) Close() (err error) {
	mu.Lock()
	defer mu.Unlock()
	c.close()
	return nil
}

func (c *Conn) close() {
	if c.closed {
		return
	}
	if !c.destroyed {
		C.utp_close(c.s)
		c.s = nil
	}
	c.closed = true
	c.cond.Broadcast()
}

func (c *Conn) LocalAddr() net.Addr {
	mu.Lock()
	defer mu.Unlock()
	return getSocketForLibContext(C.utp_get_context(c.s)).pc.LocalAddr()
}

func (c *Conn) readNoWait(b []byte) (n int, err error) {
	n = copy(b, c.readBuf)
	c.readBuf = c.readBuf[n:]
	if n != 0 && len(c.readBuf) == 0 {
		C.utp_read_drained(c.s)
	}
	err = func() error {
		switch {
		case c.gotEOF:
			return io.EOF
		case c.destroyed:
			return errors.New("destroyed")
		case c.closed:
			return errors.New("closed")
		case !c.readDeadline.IsZero() && !time.Now().Before(c.readDeadline):
			return errDeadlineExceeded{}
		default:
			return nil
		}
	}()
	return
}

func (c *Conn) Read(b []byte) (int, error) {
	mu.Lock()
	defer mu.Unlock()
	for {
		n, err := c.readNoWait(b)
		if n != 0 || len(b) == 0 || err != nil {
			return n, err
		}
		c.cond.Wait()
	}
}

func (c *Conn) Write(b []byte) (int, error) {
	mu.Lock()
	defer mu.Unlock()
	n := 0
	for len(b) != 0 {
		if c.closed {
			return n, errors.New("closed")
		}
		if c.destroyed {
			return n, errors.New("destroyed")
		}
		n1 := C.utp_write(c.s, unsafe.Pointer(&b[0]), C.size_t(len(b)))
		if n1 < 0 {
			panic(n1)
		}
		b = b[n1:]
		n += int(n1)
		if n1 == 0 {
			if !c.writeDeadline.IsZero() && !time.Now().Before(c.writeDeadline) {
				return n, errDeadlineExceeded{}
			}
			c.cond.Wait()
		}
	}
	return n, nil
}

func (c *Conn) RemoteAddr() net.Addr {
	var rsa syscall.RawSockaddrAny
	var addrlen C.socklen_t = syscall.SizeofSockaddrAny
	C.utp_getpeername(c.s, (*C.struct_sockaddr)(unsafe.Pointer(&rsa)), &addrlen)
	sa, err := anyToSockaddr(&rsa)
	if err != nil {
		panic(err)
	}
	return sockaddrToUDP(sa)
}

func (c *Conn) SetDeadline(t time.Time) error {
	mu.Lock()
	defer mu.Unlock()
	c.readDeadline = t
	c.writeDeadline = t
	c.cond.Broadcast()
	return nil
}
func (c *Conn) SetReadDeadline(t time.Time) error {
	mu.Lock()
	defer mu.Unlock()
	c.readDeadline = t
	c.cond.Broadcast()
	return nil
}
func (c *Conn) SetWriteDeadline(t time.Time) error {
	mu.Lock()
	defer mu.Unlock()
	c.writeDeadline = t
	c.cond.Broadcast()
	return nil
}

func (c *Conn) setGotEOF() {
	c.gotEOF = true
	c.cond.Broadcast()
}

func (c *Conn) onDestroyed() {
	c.destroyed = true
	c.s = nil
	c.cond.Broadcast()
}
