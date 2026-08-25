package utp

import (
	"net"

	"github.com/anacrolix/go-libutp/pureutp"
)

// Pure is the pure Go implementation, [github.com/anacrolix/go-libutp/pureutp]. It is available
// whatever the build selected as [Default].
var Pure Implementation = &implementation{"pureutp", newPureSocketFromPacketConn}

// Adapts a pureutp Socket to the Socket interface. Everything but the firewall callback, whose
// type differs, is already the right shape.
type pureSocket struct {
	*pureutp.Socket
}

var _ Socket = pureSocket{}

func (me pureSocket) SetFirewallCallback(f FirewallCallback) {
	if f == nil {
		me.Socket.SetFirewallCallback(nil)
		return
	}
	me.Socket.SetFirewallCallback(pureutp.FirewallCallback(f))
}

func newPureSocketFromPacketConn(pc net.PacketConn, opts ...Option) (Socket, error) {
	o := newOptions(opts)
	var popts []pureutp.NewSocketOpt
	if o.hasLogger {
		popts = append(popts, pureutp.WithLogger(o.logger))
	}
	if o.targetDelay != 0 {
		popts = append(popts, pureutp.WithTargetDelay(o.targetDelay))
	}
	s, err := pureutp.NewSocketFromPacketConn(pc, popts...)
	if err != nil {
		return nil, err
	}
	if o.sendBuffer != 0 {
		s.SetWriteBufferLen(o.sendBuffer)
	}
	if o.receiveBuffer != 0 {
		s.SetReadBufferLen(o.receiveBuffer)
	}
	return pureSocket{s}, nil
}
