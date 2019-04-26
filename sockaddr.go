package utp

/*
#include "utp.h"
*/
import "C"
import (
	"net"
	"strconv"
	"syscall"
	"unsafe"

	"github.com/anacrolix/missinggo/inproc"
)

func toSockaddrInet(ip net.IP, port int, zone string) (*C.struct_sockaddr, C.socklen_t) {
	if ip4 := ip.To4(); ip4 != nil && zone == "" {
		rsa := syscall.RawSockaddrInet4{
			// Len:    syscall.SizeofSockaddrInet4,
			Family: syscall.AF_INET,
			Port:   uint16(port),
		}
		if n := copy(rsa.Addr[:], ip4); n != 4 {
			panic(n)
		}
		return (*C.struct_sockaddr)(unsafe.Pointer(&rsa)), C.socklen_t(unsafe.Sizeof(rsa))
	}
	rsa := syscall.RawSockaddrInet6{
		// Len:      syscall.SizeofSockaddrInet6,
		Family:   syscall.AF_INET6,
		Scope_id: zoneToScopeId(zone),
		Port:     uint16(port),
	}
	if ip != nil {
		if n := copy(rsa.Addr[:], ip); n != 16 {
			panic(n)
		}
	}
	return (*C.struct_sockaddr)(unsafe.Pointer(&rsa)), C.socklen_t(unsafe.Sizeof(rsa))
}

func zoneToScopeId(zone string) uint32 {
	if zone == "" {
		return 0
	}
	if ifi, err := net.InterfaceByName(zone); err == nil {
		return uint32(ifi.Index)
	}
	ui64, _ := strconv.ParseUint(zone, 10, 32)
	return uint32(ui64)
}

func structSockaddrToUDPAddr(sa *C.struct_sockaddr, udp *net.UDPAddr) error {
	return anySockaddrToUdp((*syscall.RawSockaddrAny)(unsafe.Pointer(sa)), udp)
}

func anySockaddrToUdp(rsa *syscall.RawSockaddrAny, udp *net.UDPAddr) error {
	switch rsa.Addr.Family {
	case syscall.AF_INET:
		sa := (*syscall.RawSockaddrInet4)(unsafe.Pointer(rsa))
		udp.Port = int(sa.Port)
		udp.IP = append(udp.IP[:0], sa.Addr[:]...)
		return nil
	case syscall.AF_INET6:
		sa := (*syscall.RawSockaddrInet6)(unsafe.Pointer(rsa))
		udp.Port = int(sa.Port)
		udp.IP = append(udp.IP[:0], sa.Addr[:]...)
		return nil
	default:
		return syscall.EAFNOSUPPORT
	}
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

func netAddrToLibSockaddr(na net.Addr) (*C.struct_sockaddr, C.socklen_t) {
	switch v := na.(type) {
	case *net.UDPAddr:
		return toSockaddrInet(v.IP, v.Port, v.Zone)
	case inproc.Addr:
		rsa := syscall.RawSockaddrInet6{
			Port: uint16(v.Port),
		}
		return (*C.struct_sockaddr)(unsafe.Pointer(&rsa)), C.socklen_t(unsafe.Sizeof(rsa))
	default:
		panic(na)
	}
}
