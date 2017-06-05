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
)

func herp(network, addr string) (p unsafe.Pointer, sl C.socklen_t, err error) {
	ua, err := net.ResolveUDPAddr(network, addr)
	if err != nil {
		return
	}
	rsa := syscall.RawSockaddrInet6{
		Len:      syscall.SizeofSockaddrInet6,
		Family:   syscall.AF_INET6,
		Scope_id: zoneToScopeId(ua.Zone),
		Port:     uint16(ua.Port),
	}
	if n := copy(rsa.Addr[:], ua.IP); n != 16 {
		panic(n)
	}
	return unsafe.Pointer(&rsa), C.socklen_t(rsa.Len), nil
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
