package utp

/*
#cgo CPPFLAGS: -DPOSIX
#include "utp.h"

uint64_t logCallback(utp_callback_arguments *);
uint64_t acceptCallback(utp_callback_arguments *);
uint64_t sendtoCallback(utp_callback_arguments *);
uint64_t stateChangeCallback(utp_callback_arguments *);
uint64_t readCallback(utp_callback_arguments *);
*/
import "C"
import (
	"io"
	"log"
	"net"
	"reflect"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

func (ctx *C.utp_context) setCallbacks() {
	C.utp_set_callback(ctx, C.UTP_LOG, (*C.utp_callback_t)(C.logCallback))
	C.utp_set_callback(ctx, C.UTP_ON_ACCEPT, (*C.utp_callback_t)(C.acceptCallback))
	C.utp_set_callback(ctx, C.UTP_SENDTO, (*C.utp_callback_t)(C.sendtoCallback))
	C.utp_set_callback(ctx, C.UTP_ON_STATE_CHANGE, (*C.utp_callback_t)(C.stateChangeCallback))
	C.utp_set_callback(ctx, C.UTP_ON_READ, (*C.utp_callback_t)(C.readCallback))
}

func (ctx *C.utp_context) setOption(opt, val int) int {
	return int(C.utp_context_set_option(ctx, C.int(opt), C.int(val)))
}

//export sendtoCallback
func sendtoCallback(a *C.utp_callback_arguments) C.uint64 {
	log.Printf("sendto callback: %v", *a)
	s := getSocketForUtpContext(a.context)
	sa := *(**C.struct_sockaddr)(unsafe.Pointer(&a.anon0[0]))
	b := a.bufBytes()
	addr := structSockaddrToUDPAddr(sa)
	log.Println(addr, b)
	log.Println(s.pc.WriteToUDP(b, addr))
	// salen := *(*C.socklen_t)(unsafe.Pointer(&a.anon1))
	// log.Println(fd, a.buf, a.len, sa, salen, sa.sa_family)
	// d, errno := C.sendto(fd, unsafe.Pointer(a.buf), a.len, 0, sa, 16)
	// log.Println(d, errno)
	return 0
}

//export logCallback
func logCallback(a *C.utp_callback_arguments) C.uint64 {
	log.Printf("log callback: %v", *a)
	return 0
}

func (a *C.utp_callback_arguments) bufBytes() []byte {
	return *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{uintptr(unsafe.Pointer(a.buf)), int(a.len), int(a.len)}))
}

//export readCallback
func readCallback(a *C.utp_callback_arguments) C.uint64 {
	log.Printf("read callback: %v", *a)
	c := connForLibSocket(a.socket)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readBuf = append(c.readBuf, a.bufBytes()...)
	c.cond.Broadcast()
	return 0
}

//export acceptCallback
func acceptCallback(a *C.utp_callback_arguments) C.uint64 {
	log.Printf("accept callback: %v", *a)
	s := getSocketForUtpContext(a.context)
	s.pushBacklog(newConn(a.socket))
	return 0
}

func (a *C.utp_callback_arguments) state() C.int {
	return *(*C.int)(unsafe.Pointer(&a.anon0))
}

func libStateName(state C.int) string {
	return C.GoString((*[5]*C.char)(unsafe.Pointer(&C.utp_state_names))[state])
}

//export stateChangeCallback
func stateChangeCallback(a *C.utp_callback_arguments) C.uint64 {
	log.Printf("state: %d", a.state())
	log.Println(len(C.utp_state_names))
	log.Printf("state changed: socket %p: %s", a.socket, libStateName(a.state()))
	switch a.state() {
	case C.UTP_STATE_CONNECT:
		connForLibSocket(a.socket).setConnected()
	case C.UTP_STATE_WRITABLE:
	case C.UTP_STATE_EOF:
		connForLibSocket(a.socket).setGotEOF()
	case C.UTP_STATE_DESTROYING:
	default:
		panic(a.state)
	}
	return 0
}

func newConn(s *C.utp_socket) *Conn {
	c := &Conn{s: s}
	c.cond.L = &c.mu
	libSocketToConn[s] = c
	return c
}

func toSockaddr(addr net.Addr) (*C.struct_sockaddr, C.socklen_t) {
	udp := addr.(*net.UDPAddr)
	var sa syscall.RawSockaddrInet6
	sa.Port = uint16(udp.Port)
	copy(sa.Addr[:], udp.IP)
	return (*C.struct_sockaddr)(unsafe.Pointer(&sa)), C.socklen_t(unsafe.Sizeof(sa))
}

func getSocketForUtpContext(ctx *C.utp_context) *Socket {
	mu.Lock()
	defer mu.Unlock()
	return utpContextToSocket[ctx]
}

func NewSocket(port int) *Socket {
	pc, err := net.ListenUDP("udp", &net.UDPAddr{Port: port})
	if err != nil {
		panic(err)
	}
	ctx := C.utp_init(2)
	ctx.setCallbacks()
	ctx.setOption(C.UTP_LOG_NORMAL, 1)
	ctx.setOption(C.UTP_LOG_MTU, 1)
	ctx.setOption(C.UTP_LOG_DEBUG, 1)
	go func() {
		for {
			var b [0x1000]byte
			n, addr, err := pc.ReadFrom(b[:])
			if err != nil {
				panic(err)
			}
			log.Println(n, addr, err)
			log.Printf("%#v", addr)
			sa, sal := toSockaddr(addr)
			C.utp_process_udp(ctx, (*C.byte)(&b[0]), C.size_t(n), sa, sal)
			C.utp_issue_deferred_acks(ctx)
			C.utp_check_timeouts(ctx)
		}
	}()
	s := &Socket{pc, ctx, make(chan *Conn, 5)}
	mu.Lock()
	utpContextToSocket[ctx] = s
	mu.Unlock()
	return s
}

type Socket struct {
	pc      *net.UDPConn
	ctx     *C.utp_context
	backlog chan *Conn
}

var (
	mu                 sync.Mutex
	utpContextToSocket = map[*C.utp_context]*Socket{}
	libSocketToConn    = map[*C.utp_socket]*Conn{}
)

func (s *Socket) Accept() (net.Conn, error) {
	c := <-s.backlog
	return c, nil
}

func (s *Socket) Dial(addr string) (net.Conn, error) {
	c := newConn(C.utp_create_socket(s.ctx))
	rsa := syscall.RawSockaddrInet6{
		Port: 3000,
	}
	if n := copy(rsa.Addr[:], net.IPv4(127, 0, 0, 1).To16()); n != 16 {
		panic(n)
	}
	C.utp_connect(c.s, (*C.struct_sockaddr)(unsafe.Pointer(&rsa)), syscall.SizeofSockaddrInet6)
	err := c.waitForConnect()
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

func structSockaddrToUDPAddr(sa *C.struct_sockaddr) *net.UDPAddr {
	meh, err := anyToSockaddr((*syscall.RawSockaddrAny)(unsafe.Pointer(sa)))
	if err != nil {
		panic(err)
	}
	return sockaddrToUDP(meh).(*net.UDPAddr)
}

func anyToSockaddr(rsa *syscall.RawSockaddrAny) (syscall.Sockaddr, error) {
	switch rsa.Addr.Family {
	case syscall.AF_LINK:
		pp := (*syscall.RawSockaddrDatalink)(unsafe.Pointer(rsa))
		sa := new(syscall.SockaddrDatalink)
		sa.Len = pp.Len
		sa.Family = pp.Family
		sa.Index = pp.Index
		sa.Type = pp.Type
		sa.Nlen = pp.Nlen
		sa.Alen = pp.Alen
		sa.Slen = pp.Slen
		for i := 0; i < len(sa.Data); i++ {
			sa.Data[i] = pp.Data[i]
		}
		return sa, nil

	case syscall.AF_UNIX:
		pp := (*syscall.RawSockaddrUnix)(unsafe.Pointer(rsa))
		if pp.Len < 2 || pp.Len > syscall.SizeofSockaddrUnix {
			return nil, syscall.EINVAL
		}
		sa := new(syscall.SockaddrUnix)

		// Some BSDs include the trailing NUL in the length, whereas
		// others do not. Work around this by subtracting the leading
		// family and len. The path is then scanned to see if a NUL
		// terminator still exists within the length.
		n := int(pp.Len) - 2 // subtract leading Family, Len
		for i := 0; i < n; i++ {
			if pp.Path[i] == 0 {
				// found early NUL; assume Len included the NUL
				// or was overestimating.
				n = i
				break
			}
		}
		bytes := (*[10000]byte)(unsafe.Pointer(&pp.Path[0]))[0:n]
		sa.Name = string(bytes)
		return sa, nil

	case syscall.AF_INET:
		pp := (*syscall.RawSockaddrInet4)(unsafe.Pointer(rsa))
		sa := new(syscall.SockaddrInet4)
		// p := (*[2]byte)(unsafe.Pointer(&pp.Port))
		// sa.Port = int(p[0])<<8 + int(p[1])
		// I don't know why the port isn't reversed when it comes from utp.
		sa.Port = int(pp.Port)
		for i := 0; i < len(sa.Addr); i++ {
			sa.Addr[i] = pp.Addr[i]
		}
		return sa, nil

	case syscall.AF_INET6:
		pp := (*syscall.RawSockaddrInet6)(unsafe.Pointer(rsa))
		sa := new(syscall.SockaddrInet6)
		// p := (*[2]byte)(unsafe.Pointer(&pp.Port))
		// sa.Port = int(p[0])<<8 + int(p[1])
		// I don't know why the port isn't reversed when it comes from utp.
		sa.Port = int(pp.Port)
		sa.ZoneId = pp.Scope_id
		for i := 0; i < len(sa.Addr); i++ {
			sa.Addr[i] = pp.Addr[i]
		}
		return sa, nil
	}
	return nil, syscall.EAFNOSUPPORT
}

func sockaddrToUDP(sa syscall.Sockaddr) net.Addr {
	switch sa := sa.(type) {
	case *syscall.SockaddrInet4:
		return &net.UDPAddr{IP: sa.Addr[0:], Port: sa.Port}
	case *syscall.SockaddrInet6:
		return &net.UDPAddr{IP: sa.Addr[0:], Port: sa.Port /*Zone: zoneToString(int(sa.ZoneId))*/}
	}
	return nil
}

type Conn struct {
	s          *C.utp_socket
	mu         sync.Mutex
	cond       sync.Cond
	readBuf    []byte
	gotEOF     bool
	gotConnect bool
	writeBuf   []byte
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
	C.utp_close(c.s)
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
		if len(c.readBuf) == 0 {
			C.utp_read_drained(c.s)
		}
		if len(c.readBuf) == 0 && c.gotEOF {
			return n, io.EOF
		}
		if n != 0 || len(b) == 0 {
			return n, nil
		}
		c.cond.Wait()
	}
}

func (c *Conn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for len(b) != 0 {
		n1 := C.utp_write(c.s, unsafe.Pointer(&b[0]), C.size_t(len(b)))
		if n1 < 0 {
			panic(n1)
		}
		b = b[n1:]
		n += int(n1)
		if n1 == 0 {
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

func (c *Conn) SetDeadline(time.Time) error      { return nil }
func (c *Conn) SetReadDeadline(time.Time) error  { return nil }
func (c *Conn) SetWriteDeadline(time.Time) error { return nil }

func connForLibSocket(us *C.utp_socket) *Conn {
	return libSocketToConn[us]
}

func (c *Conn) setGotEOF() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gotEOF = true
	c.cond.Broadcast()
}
