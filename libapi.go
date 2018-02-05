package utp

/*
#include "utp.h"
*/
import "C"
import "sync"

type Option = C.int

const (
	LogNormal   Option = C.UTP_LOG_NORMAL
	LogMtu      Option = C.UTP_LOG_MTU
	LogDebug    Option = C.UTP_LOG_DEBUG
	SendBuffer  Option = C.UTP_SNDBUF
	RecvBuffer  Option = C.UTP_RCVBUF
	TargetDelay Option = C.UTP_TARGET_DELAY
)

var (
	mu                 sync.Mutex
	libContextToSocket = map[*C.utp_context]*Socket{}
)

func getSocketForLibContext(uc *C.utp_context) *Socket {
	return libContextToSocket[uc]
}
