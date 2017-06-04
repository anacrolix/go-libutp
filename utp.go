package utp

/*
#cgo CPPFLAGS: -DPOSIX
#include <errno.h>
#include <stdio.h>
#include "utp.h"
uint64_t logCallback(utp_callback_arguments *);
uint64_t acceptCallback(utp_callback_arguments *);
uint64_t sendtoCallback(utp_callback_arguments *);

static ssize_t sendto_fd_c(int fd, utp_callback_arguments *a) {
    printf("sa_len %d, address_len %d, fd %d, sa_family %d\n", a->address->sa_len, a->address_len, fd, a->address->sa_family);
    ssize_t ret = sendto(fd, a->buf, a->len, 0, a->address, a->address_len);
    fprintf(stderr, "errno %d\n", errno);
    return ret;
}
*/
import "C"
import (
	"log"
	"net"
	"reflect"
	"sync"
	"syscall"
	"unsafe"
)

func (ctx *C.utp_context) setCallbacks() {
	C.utp_set_callback(ctx, C.UTP_LOG, (*C.utp_callback_t)(C.logCallback))
	C.utp_set_callback(ctx, C.UTP_ON_ACCEPT, (*C.utp_callback_t)(C.acceptCallback))
	C.utp_set_callback(ctx, C.UTP_SENDTO, (*C.utp_callback_t)(C.sendtoCallback))
}

func (ctx *C.utp_context) setOption(opt, val int) int {
	return int(C.utp_context_set_option(ctx, C.int(opt), C.int(val)))
}

//export sendtoCallback
func sendtoCallback(a *C.utp_callback_arguments) C.uint64 {
	log.Printf("sendto callback: %v", *a)
	s := getSocketForUtpContext(a.context)
	sa := *(**C.struct_sockaddr)(unsafe.Pointer(&a.anon0[0]))
	b := *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{uintptr(unsafe.Pointer(a.buf)), int(a.len), int(a.len)}))
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

//export acceptCallback
func acceptCallback(a *C.utp_callback_arguments) C.uint64 {
	log.Printf("accept callback: %v", *a)
	s := a.socket
	var sa syscall.RawSockaddrInet6
	sal := C.socklen_t(unsafe.Sizeof(sa))
	C.utp_getpeername(s, (*C.struct_sockaddr)(unsafe.Pointer(&sa)), &sal)
	log.Printf("%#v", sa)
	return 0
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

func NewSocket() *Socket {
	pc, err := net.ListenUDP("udp", &net.UDPAddr{Port: 3000})
	if err != nil {
		panic(err)
	}
	f, _ := pc.File()
	log.Println("fd", f.Fd())
	log.Printf("listening at %s", pc.LocalAddr())
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
		}
	}()
	s := &Socket{pc, ctx}
	mu.Lock()
	utpContextToSocket[ctx] = s
	mu.Unlock()
	return s
}

type Socket struct {
	pc  *net.UDPConn
	ctx *C.utp_context
}

var (
	mu                 sync.Mutex
	utpContextToSocket = map[*C.utp_context]*Socket{}
)

func (s *Socket) Accept() (net.Conn, error) {
	select {}
	return nil, nil
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
