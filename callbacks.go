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
	// log.Printf("sendto callback: socket=%p", a.socket)
	s := getSocketForUtpContext(a.context)
	sa := *(**C.struct_sockaddr)(unsafe.Pointer(&a.anon0[0]))
	b := a.bufBytes()
	addr := structSockaddrToUDPAddr(sa)
	log.Printf("sending %d bytes to %s", len(b), addr)
	// log.Println(s.Addr().Network())
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
	log.Printf("error callback: socket %p: %s", a.socket, libErrorCodeNames(a.error_code()))
	return 0
}

//export logCallback
func logCallback(a *C.utp_callback_arguments) C.uint64 {
	log.Printf("libutp: %s", C.GoString((*C.char)(unsafe.Pointer(a.buf))))
	return 0
}

//export stateChangeCallback
func stateChangeCallback(a *C.utp_callback_arguments) C.uint64 {
	log.Printf("state changed: socket %p: %s", a.socket, libStateName(a.state()))
	// Socket is always set for this callback.
	c := connForLibSocket(a.socket)
	switch a.state() {
	case C.UTP_STATE_CONNECT:
		c.setConnected()
	case C.UTP_STATE_WRITABLE:
		c.mu.Lock()
		c.cond.Broadcast()
		c.mu.Unlock()
	case C.UTP_STATE_EOF:
		c.setGotEOF()
	case C.UTP_STATE_DESTROYING:
		c.onStateDestroying()
	default:
		panic(a.state)
	}
	return 0
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
	log.Printf("accept callback: %#v", *a)
	s := getSocketForUtpContext(a.context)
	s.pushBacklog(newConn(a.socket))
	return 0
}
