package utp

import (
	"net"

	"github.com/anacrolix/go-libutp/purego"
)

// Purego is the pure Go implementation, [github.com/anacrolix/go-libutp/purego]. It is available
// whatever the build selected as [Default].
var Purego Implementation = &implementation{"purego", newPuregoSocket}

// Adapts a purego Socket to the Socket interface. Everything but the firewall callback, whose
// type differs, is already the right shape.
type puregoSocket struct {
	*purego.Socket
}

var _ Socket = puregoSocket{}

func (me puregoSocket) SetFirewallCallback(f FirewallCallback) {
	if f == nil {
		me.Socket.SetFirewallCallback(nil)
		return
	}
	me.Socket.SetFirewallCallback(purego.FirewallCallback(f))
}

func newPuregoSocket(pc net.PacketConn, opts ...Option) (Socket, error) {
	o := newOptions(opts)
	var popts []purego.NewSocketOpt
	if o.logger != nil {
		popts = append(popts, purego.WithLogger(o.logger))
	}
	if o.targetDelay != 0 {
		popts = append(popts, purego.WithTargetDelay(o.targetDelay))
	}
	s, err := purego.NewSocketFromPacketConn(pc, popts...)
	if err != nil {
		return nil, err
	}
	if o.sendBuffer != 0 {
		s.SetWriteBufferLen(o.sendBuffer)
	}
	if o.receiveBuffer != 0 {
		s.SetReadBufferLen(o.receiveBuffer)
	}
	return puregoSocket{s}, nil
}
