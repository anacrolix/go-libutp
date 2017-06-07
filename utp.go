package utp

/*
#cgo CPPFLAGS: -DPOSIX
#include "utp.h"

uint64_t errorCallback(utp_callback_arguments *);
uint64_t logCallback(utp_callback_arguments *);
uint64_t acceptCallback(utp_callback_arguments *);
uint64_t sendtoCallback(utp_callback_arguments *);
uint64_t stateChangeCallback(utp_callback_arguments *);
uint64_t readCallback(utp_callback_arguments *);
*/
import "C"
import (
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type socklen C.socklen_t

func (ctx *C.utp_context) setCallbacks() {
	C.utp_set_callback(ctx, C.UTP_LOG, (*C.utp_callback_t)(C.logCallback))
	C.utp_set_callback(ctx, C.UTP_ON_ACCEPT, (*C.utp_callback_t)(C.acceptCallback))
	C.utp_set_callback(ctx, C.UTP_SENDTO, (*C.utp_callback_t)(C.sendtoCallback))
	C.utp_set_callback(ctx, C.UTP_ON_STATE_CHANGE, (*C.utp_callback_t)(C.stateChangeCallback))
	C.utp_set_callback(ctx, C.UTP_ON_READ, (*C.utp_callback_t)(C.readCallback))
	C.utp_set_callback(ctx, C.UTP_ON_ERROR, (*C.utp_callback_t)(C.errorCallback))
}

func (ctx *C.utp_context) setOption(opt, val int) int {
	return int(C.utp_context_set_option(ctx, C.int(opt), C.int(val)))
}

func libStateName(state C.int) string {
	return C.GoString((*[5]*C.char)(unsafe.Pointer(&C.utp_state_names))[state])
}

func libErrorCodeNames(error_code C.int) string {
	return C.GoString((*[3]*C.char)(unsafe.Pointer(&C.utp_state_names))[error_code])
}

func newConn(s *C.utp_socket) *Conn {
	c := &Conn{s: s}
	c.cond.L = &c.mu
	libSocketToConn[s] = c
	return c
}

func getSocketForUtpContext(ctx *C.utp_context) *Socket {
	mu.Lock()
	defer mu.Unlock()
	return utpContextToSocket[ctx]
}

func (s *Socket) packetReader() {
	var b [0x1000]byte
	for {
		n, addr, err := s.pc.ReadFrom(b[:])
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			panic(err)
		}
		log.Println(n, addr, err)
		// log.Printf("%#v", addr)
		sa, sal := netAddrToLibSockaddr(addr)
		func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.closed {
				return
			}
			C.utp_process_udp(s.ctx, (*C.byte)(&b[0]), C.size_t(n), sa, sal)
			C.utp_issue_deferred_acks(s.ctx)
		}()
	}
}

func (s *Socket) timeoutChecker() {
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		C.utp_check_timeouts(s.ctx)
		s.mu.Unlock()
		time.Sleep(500 * time.Millisecond)
	}
}

func NewSocket(network, addr string) (*Socket, error) {
	pc, err := net.ListenPacket(network, addr)
	if err != nil {
		return nil, err
	}
	ctx := C.utp_init(2)
	ctx.setCallbacks()
	// ctx.setOption(C.UTP_LOG_NORMAL, 1)
	// ctx.setOption(C.UTP_LOG_MTU, 1)
	// ctx.setOption(C.UTP_LOG_DEBUG, 1)
	s := &Socket{
		pc:      pc,
		ctx:     ctx,
		backlog: make(chan *Conn, 5),
	}
	mu.Lock()
	utpContextToSocket[ctx] = s
	mu.Unlock()
	go s.packetReader()
	return s, nil
}

type Socket struct {
	mu      sync.Mutex
	pc      net.PacketConn
	ctx     *C.utp_context
	backlog chan *Conn
	closed  bool
}

func (me *Socket) Close() error {
	me.mu.Lock()
	defer me.mu.Unlock()
	if me.closed {
		return nil
	}
	log.Print("destroy socket")
	C.utp_destroy(me.ctx)
	me.ctx = nil
	me.pc.Close()
	close(me.backlog)
	me.closed = true
	return nil
}

func (me *Socket) Addr() net.Addr {
	return me.pc.LocalAddr()
}

var (
	mu                 sync.Mutex
	utpContextToSocket = map[*C.utp_context]*Socket{}
	libSocketToConn    = map[*C.utp_socket]*Conn{}
)

func (s *Socket) Accept() (net.Conn, error) {
	log.Print("socket accept")
	c := <-s.backlog
	return c, nil
}

func (s *Socket) Dial(addr string) (net.Conn, error) {
	ua, err := net.ResolveUDPAddr(s.Addr().Network(), addr)
	if err != nil {
		panic(err)
	}
	sa, sl := netAddrToLibSockaddr(ua)
	c := newConn(C.utp_create_socket(s.ctx))
	C.utp_connect(c.s, sa, sl)
	err = c.waitForConnect()
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (s *Socket) pushBacklog(c *Conn) {
	select {
	case s.backlog <- c:
	default:
		c.Close()
	}
}

type Conn struct {
	s          *C.utp_socket
	mu         sync.Mutex
	cond       sync.Cond
	readBuf    []byte
	gotEOF     bool
	gotConnect bool
	writeBuf   []byte
	// Set on state changed to UTP_STATE_DESTROYING. Not valid to refer to the
	// socket after getting this.
	destroyed bool
	// Conn.Close was called.
	closed bool

	writeDeadline time.Time
	readDeadline  time.Time
}

func (c *Conn) setConnected() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gotConnect = true
	c.cond.Broadcast()
}

func (c *Conn) waitForConnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for !c.gotConnect {
		c.cond.Wait()
	}
	return nil
}

func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	log.Print("closing conn")
	C.utp_close(c.s)
	c.s = nil
	c.closed = true
	c.cond.Broadcast()
	return nil
}

func (c *Conn) LocalAddr() net.Addr {
	return getSocketForUtpContext(C.utp_get_context(c.s)).pc.LocalAddr()
}

func (c *Conn) Read(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		n := copy(b, c.readBuf)
		c.readBuf = c.readBuf[n:]
		log.Println(n, len(c.readBuf))
		if n != 0 && len(c.readBuf) == 0 {
			C.utp_read_drained(c.s)
		}
		if len(c.readBuf) == 0 && c.gotEOF {
			return n, io.EOF
		}
		if n != 0 || len(b) == 0 {
			return n, nil
		}
		if c.destroyed {
			return 0, errors.New("destroyed")
		}
		if c.closed {
			return 0, errors.New("closed")
		}
		if !c.readDeadline.IsZero() && !time.Now().Before(c.readDeadline) {
			return 0, errDeadlineExceeded{}
		}
		c.cond.Wait()
	}
}

func (c *Conn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for len(b) != 0 {
		if c.closed {
			return n, errors.New("closed")
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
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t
	c.writeDeadline = t
	c.cond.Broadcast()
	return nil
}
func (c *Conn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t
	c.cond.Broadcast()
	return nil
}
func (c *Conn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeDeadline = t
	c.cond.Broadcast()
	return nil
}

func connForLibSocket(us *C.utp_socket) *Conn {
	return libSocketToConn[us]
}

func (c *Conn) setGotEOF() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gotEOF = true
	c.cond.Broadcast()
}

func (c *Conn) onStateDestroying() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.destroyed = true
	c.cond.Broadcast()
}
