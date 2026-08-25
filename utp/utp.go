// Package utp provides one interface over both µTP implementations in this module, and picks one
// of them at build time.
//
// By default it uses libutp, the C++ reference implementation, through cgo. Building with the
// purego tag, or with cgo disabled, uses the pure Go implementation in
// [github.com/anacrolix/go-libutp/pureutp] instead:
//
//	go build -tags purego ./...
//	CGO_ENABLED=0 go build ./...
//
// [Default] is whichever one the build selected, and [NewSocket] is shorthand for
// Default.NewSocket. Code that wants a particular implementation rather than the default can name
// [Pure] or [Libutp] directly; Libutp only exists where libutp is being compiled, which is to say
// not under the purego tag and not with cgo disabled.
//
// Implementations are values, so code that has to work with either can take an [Implementation]
// and be handed one, rather than choosing at its own build time.
package utp

import (
	"context"
	"net"
	"time"

	"github.com/anacrolix/log"
)

// An Implementation makes Sockets. The two in this module are [Pure] and [Libutp], and [Default]
// is whichever the build selected.
type Implementation interface {
	// Name is "pureutp" or "libutp".
	Name() string
	// NewSocket listens on the given network and address. Only UDP networks are supported; for
	// anything else, listen yourself and use NewSocketFromPacketConn.
	NewSocket(network, addr string, opts ...Option) (Socket, error)
	// NewSocketFromPacketConn runs µTP over a PacketConn you already have. The Socket takes
	// ownership of it: closing the Socket closes the PacketConn.
	NewSocketFromPacketConn(pc net.PacketConn, opts ...Option) (Socket, error)
}

// A FirewallCallback reports whether an incoming connection should be ignored. Rejecting one this
// way is better than accepting and immediately closing it, because the peer sees no response at
// all rather than an acknowledgement followed by a reset.
//
// It is called while the Socket holds its own lock, so it must not block or call back into the
// Socket.
type FirewallCallback func(net.Addr) bool

// Socket is a µTP endpoint: it owns a packet conn and multiplexes connections over it.
//
// It is a [net.Listener], so Accept hands out incoming connections, and a [net.PacketConn], whose
// ReadFrom and WriteTo carry the datagrams on the port that aren't µTP. That's what lets one port
// serve µTP and another protocol, a DHT say, at the same time. Datagrams that aren't read
// promptly are dropped.
//
// The connections Accept and Dial return are ordinary [net.Conn]s, deadlines included. Deadlines
// on the Socket itself, which only affect ReadFrom and WriteTo, are not supported by libutp: it
// returns an error wrapping [errors.ErrUnsupported] from the three deadline methods.
type Socket interface {
	net.Listener
	net.PacketConn

	// Dial connects to addr on the Socket's own network.
	Dial(addr string) (net.Conn, error)
	// DialTimeout connects to addr, giving up after timeout. A zero timeout means no limit.
	DialTimeout(addr string, timeout time.Duration) (net.Conn, error)
	// DialContext connects to addr. An empty network means the Socket's own.
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)

	// SetFirewallCallback sets the function consulted before each incoming connection is
	// accepted. Pass nil to accept everything, which is the default.
	SetFirewallCallback(FirewallCallback)

	// ReadBufferLen and WriteBufferLen report the buffer sizes new connections are given. The
	// write buffer also caps the congestion window; the read buffer is what the receive window
	// advertised to peers is computed from. Connections already open keep the sizes they were
	// created with.
	ReadBufferLen() int
	WriteBufferLen() int
	SetReadBufferLen(int)
	SetWriteBufferLen(int)

	// SetLogging turns the protocol's own log categories on and off. They go to the Socket's
	// logger, are all off by default, and are noisy: normal covers connection lifecycle and
	// packet loss, mtu the path MTU search, and debug every packet. libutp compiles its debug
	// logging out, so debug does nothing there.
	SetLogging(normal, mtu, debug bool)
}

// The options both implementations have in common. Anything left zero is left at the
// implementation's own default.
type options struct {
	logger        log.Logger
	hasLogger     bool
	sendBuffer    int
	receiveBuffer int
	targetDelay   time.Duration
}

// An Option configures a Socket as it's created.
type Option func(*options)

// WithLogger gives a Socket its own logger, instead of the implementation's package level one:
// [github.com/anacrolix/go-libutp.Logger] or [github.com/anacrolix/go-libutp/pureutp.Logger].
// This is the logger the categories in [Socket.SetLogging] write to, along with anything either
// implementation has to report about the socket itself.
func WithLogger(l log.Logger) Option {
	return func(o *options) {
		o.logger = l
		o.hasLogger = true
	}
}

// WithBufferSizes sets the send and receive buffers new connections are given, in bytes. Zero
// leaves either at the default, which is a megabyte.
func WithBufferSizes(send, receive int) Option {
	return func(o *options) {
		o.sendBuffer = send
		o.receiveBuffer = receive
	}
}

// WithTargetDelay sets the one way queuing delay the congestion controller aims for. Lower means
// yielding sooner to whatever else is sharing the link. Zero leaves it at the default of 100ms,
// which is what the rest of the µTP network uses.
func WithTargetDelay(d time.Duration) Option {
	return func(o *options) { o.targetDelay = d }
}

func newOptions(opts []Option) (o options) {
	for _, opt := range opts {
		opt(&o)
	}
	return
}

// Both implementations build their sockets on a PacketConn and listen the same way, so they only
// have to provide the constructor that takes one.
//
// Used through a pointer so that Implementation values are comparable: Default == Libutp is a
// reasonable thing to write, and a struct with a func field in it would panic.
type implementation struct {
	name              string
	newFromPacketConn func(pc net.PacketConn, opts ...Option) (Socket, error)
}

func (me *implementation) Name() string   { return me.name }
func (me *implementation) String() string { return me.name }

func (me *implementation) NewSocketFromPacketConn(pc net.PacketConn, opts ...Option) (Socket, error) {
	return me.newFromPacketConn(pc, opts...)
}

func (me *implementation) NewSocket(network, addr string, opts ...Option) (Socket, error) {
	pc, err := net.ListenPacket(network, addr)
	if err != nil {
		return nil, err
	}
	s, err := me.newFromPacketConn(pc, opts...)
	if err != nil {
		pc.Close()
		return nil, err
	}
	return s, nil
}

// NewSocket listens on the given network and address using [Default]. Only UDP networks are
// supported; for anything else, listen yourself and use [NewSocketFromPacketConn].
func NewSocket(network, addr string, opts ...Option) (Socket, error) {
	return Default.NewSocket(network, addr, opts...)
}

// NewSocketFromPacketConn runs µTP over a PacketConn you already have, using [Default]. The
// Socket takes ownership of it: closing the Socket closes the PacketConn.
func NewSocketFromPacketConn(pc net.PacketConn, opts ...Option) (Socket, error) {
	return Default.NewSocketFromPacketConn(pc, opts...)
}
