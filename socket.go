package utp

/*
#include "utp.h"
*/
import "C"
import (
	"errors"
	"log"
	"net"
	"time"
)

type Socket struct {
	pc            net.PacketConn
	ctx           *C.utp_context
	backlog       chan *Conn
	closed        bool
	conns         map[*C.utp_socket]*Conn
	nonUtpReads   chan packet
	writeDeadline time.Time
	readDeadline  time.Time
}

var (
	_ net.PacketConn = (*Socket)(nil)
)

type packet struct {
	b    []byte
	from net.Addr
}

func NewSocket(network, addr string) (*Socket, error) {
	pc, err := net.ListenPacket(network, addr)
	if err != nil {
		return nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	ctx := C.utp_init(2)
	if ctx == nil {
		panic(ctx)
	}
	ctx.setCallbacks()
	// ctx.setOption(C.UTP_LOG_NORMAL, 1)
	// ctx.setOption(C.UTP_LOG_MTU, 1)
	// ctx.setOption(C.UTP_LOG_DEBUG, 1)
	s := &Socket{
		pc:          pc,
		ctx:         ctx,
		backlog:     make(chan *Conn, 5),
		conns:       make(map[*C.utp_socket]*Conn),
		nonUtpReads: make(chan packet, 10),
	}
	libContextToSocket[ctx] = s
	go s.packetReader()
	return s, nil
}

func (s *Socket) onLibSocketDestroyed(ls *C.utp_socket) {
	delete(s.conns, ls)
}

func (s *Socket) newConn(us *C.utp_socket) *Conn {
	c := &Conn{
		s: us,
	}
	c.cond.L = &mu
	s.conns[us] = c
	return c
}

func (s *Socket) packetReader() {
	for {
		b := make([]byte, 0x1000)
		n, addr, err := s.pc.ReadFrom(b)
		if err != nil {
			mu.Lock()
			closed := s.closed
			mu.Unlock()
			if closed {
				return
			}
			panic(err)
		}
		sa, sal := netAddrToLibSockaddr(addr)
		func() {
			mu.Lock()
			defer mu.Unlock()
			if s.closed {
				return
			}
			if C.utp_process_udp(s.ctx, (*C.byte)(&b[0]), C.size_t(n), sa, sal) != 0 {
				socketUtpPacketsReceived.Add(1)
			} else {
				s.onReadNonUtp(b[:n], addr)
			}
			C.utp_issue_deferred_acks(s.ctx)
			C.utp_check_timeouts(s.ctx)
		}()
	}
}

func (s *Socket) timeoutChecker() {
	for {
		mu.Lock()
		if s.closed {
			mu.Unlock()
			return
		}
		C.utp_check_timeouts(s.ctx)
		mu.Unlock()
		time.Sleep(500 * time.Millisecond)
	}
}

func (me *Socket) Close() error {
	mu.Lock()
	defer mu.Unlock()
	if me.closed {
		return nil
	}
	C.utp_destroy(me.ctx)
	me.ctx = nil
	me.pc.Close()
	close(me.backlog)
	close(me.nonUtpReads)
	me.closed = true
	return nil
}

func (me *Socket) Addr() net.Addr {
	return me.pc.LocalAddr()
}

func (me *Socket) LocalAddr() net.Addr {
	return me.pc.LocalAddr()
}

func (s *Socket) Accept() (net.Conn, error) {
	nc, ok := <-s.backlog
	if !ok {
		return nil, errors.New("closed")
	}
	return nc, nil
}

func (s *Socket) Dial(addr string) (net.Conn, error) {
	return s.DialTimeout(addr, 0)
}

func (s *Socket) DialTimeout(addr string, timeout time.Duration) (net.Conn, error) {
	ua, err := net.ResolveUDPAddr(s.Addr().Network(), addr)
	if err != nil {
		panic(err)
	}
	sa, sl := netAddrToLibSockaddr(ua)
	mu.Lock()
	c := s.newConn(C.utp_create_socket(s.ctx))
	C.utp_connect(c.s, sa, sl)
	defer mu.Unlock()
	err = c.waitForConnect()
	if err != nil {
		c.close()
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

func (s *Socket) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	p, ok := <-s.nonUtpReads
	if !ok {
		err = errors.New("closed")
		return
	}
	n = copy(b, p.b)
	addr = p.from
	return
}

func (s *Socket) onReadNonUtp(b []byte, from net.Addr) {
	socketNonUtpPacketsReceived.Add(1)
	select {
	case s.nonUtpReads <- packet{b, from}:
	default:
		log.Printf("dropped non utp packet: no room in buffer")
		nonUtpPacketsDropped.Add(1)
	}
}

func (s *Socket) SetReadDeadline(t time.Time) error {
	return errors.New("not implemented")
}

func (s *Socket) SetWriteDeadline(t time.Time) error {
	return errors.New("not implemented")
}

func (s *Socket) SetDeadline(t time.Time) error {
	return errors.New("not implemented")
}

func (s *Socket) WriteTo(b []byte, addr net.Addr) (int, error) {
	return s.pc.WriteTo(b, addr)
}
