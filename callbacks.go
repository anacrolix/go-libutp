package utp

/*
#include "utp.h"
*/
import "C"
import (
	"log"
	"reflect"
	"unsafe"
)

func (a *C.utp_callback_arguments) bufBytes() []byte {
	return *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{uintptr(unsafe.Pointer(a.buf)), int(a.len), int(a.len)}))
}

func (a *C.utp_callback_arguments) state() C.int {
	return *(*C.int)(unsafe.Pointer(&a.anon0))
}

func (a *C.utp_callback_arguments) error_code() C.int {
	return *(*C.int)(unsafe.Pointer(&a.anon0))
}

//export sendtoCallback
func sendtoCallback(a *C.utp_callback_arguments) (ret C.uint64) {
	s := getSocketForLibContext(a.context)
	sa := *(**C.struct_sockaddr)(unsafe.Pointer(&a.anon0[0]))
	b := a.bufBytes()
	addr := structSockaddrToUDPAddr(sa)
	n, err := s.pc.WriteTo(b, addr)
	if err != nil {
		log.Printf("error sending packet: %s", err)
		return
	}
	if n != len(b) {
		log.Printf("expected to send %d bytes but only sent %d", len(b), n)
	}
	return
}

//export errorCallback
func errorCallback(a *C.utp_callback_arguments) C.uint64 {
	codeName := libErrorCodeNames(a.error_code())
	// log.Printf("error callback: socket %p: %s", a.socket, codeName)
	libContextToSocket[a.context].conns[a.socket].onLibError(codeName)
	return 0
}

//export logCallback
func logCallback(a *C.utp_callback_arguments) C.uint64 {
	log.Printf("libutp: %s", C.GoString((*C.char)(unsafe.Pointer(a.buf))))
	return 0
}

//export stateChangeCallback
func stateChangeCallback(a *C.utp_callback_arguments) C.uint64 {
	// log.Printf("state changed: socket %p: %s", a.socket, libStateName(a.state()))
	s := libContextToSocket[a.context]
	c := s.conns[a.socket]
	switch a.state() {
	case C.UTP_STATE_CONNECT:
		c.setConnected()
		// A dialled connection will not tell the remote it's ready until it
		// writes. If the dialer has no intention of writing, this will stall
		// everything. We do an empty write to get things rolling again. This
		// circumstance occurs when c1 in the RacyRead nettest is the dialer.
		C.utp_write(a.socket, nil, 0)
	case C.UTP_STATE_WRITABLE:
		c.cond.Broadcast()
	case C.UTP_STATE_EOF:
		c.setGotEOF()
	case C.UTP_STATE_DESTROYING:
		c.onDestroyed()
		s.onLibSocketDestroyed(a.socket)
	default:
		panic(a.state)
	}
	return 0
}

//export readCallback
func readCallback(a *C.utp_callback_arguments) C.uint64 {
	s := libContextToSocket[a.context]
	c := s.conns[a.socket]
	c.readBuf = append(c.readBuf, a.bufBytes()...)
	c.cond.Broadcast()
	return 0
}

//export acceptCallback
func acceptCallback(a *C.utp_callback_arguments) C.uint64 {
	// log.Printf("accept callback: %#v", *a)
	s := getSocketForLibContext(a.context)
	s.pushBacklog(s.newConn(a.socket))
	return 0
}
