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
	closed   bool
	libError error

	writeDeadline time.Time
	readDeadline  time.Time
}

func (c *Conn) onLibError(codeName string) {
	if c.libError != nil {
		panic("multiple lib errors")
	}
	c.libError = errors.New(codeName)
	c.cond.Broadcast()
}

func (c *Conn) setConnected() {
	c.gotConnect = true
	c.cond.Broadcast()
}

func (c *Conn) waitForConnect() error {
	for {
		if c.libError != nil {
			return c.libError
		}
		if c.gotConnect {
			return nil
		}
		c.cond.Wait()
	}
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
		// Can we call this if the utp_socket is closed, destroyed or errored?
		C.utp_read_drained(c.s)
	}
	err = func() error {
		switch {
		case c.gotEOF:
			return io.EOF
		case c.libError != nil:
			return c.libError
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

func (c *Conn) writeNoWait(b []byte) (n int, err error) {
	err = func() error {
		switch {
		case c.libError != nil:
			return c.libError
		case c.closed:
			return errors.New("closed")
		case c.destroyed:
			return errors.New("destroyed")
		default:
			return nil
		}
	}()
	if err != nil {
		return
	}
	n = int(C.utp_write(c.s, unsafe.Pointer(&b[0]), C.size_t(len(b))))
	if n < 0 {
		panic(n)
	}
	return
}

func (c *Conn) Write(b []byte) (n int, err error) {
	mu.Lock()
	defer mu.Unlock()
	for len(b) != 0 {
		var n1 int
		n1, err = c.writeNoWait(b)
		b = b[n1:]
		n += n1
		if err != nil {
			break
		}
		if n1 != 0 {
			continue
		}
		if !c.writeDeadline.IsZero() && !time.Now().Before(c.writeDeadline) {
			err = errDeadlineExceeded{}
			break
		}
		c.cond.Wait()
	}
	return
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
