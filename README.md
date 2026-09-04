# go-libutp

[![Go Reference](https://pkg.go.dev/badge/github.com/anacrolix/go-libutp.svg)](https://pkg.go.dev/github.com/anacrolix/go-libutp)
[![Go](https://github.com/anacrolix/go-libutp/actions/workflows/go.yml/badge.svg)](https://github.com/anacrolix/go-libutp/actions/workflows/go.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/anacrolix/go-libutp)](https://goreportcard.com/report/github.com/anacrolix/go-libutp)

A Go wrapper for [libutp](https://github.com/bittorrent/libutp), BitTorrent's reference
implementation of the Micro Transport Protocol (µTP). µTP is a reliable, ordered, stream-oriented
transport that runs over UDP and backs off in the presence of competing traffic, so bulk transfers
don't saturate the link they share with interactive traffic.

The libutp sources are vendored in this repository, so there's no external C library to install.
Building requires cgo and a C++ compiler.

## Install

```sh
go get github.com/anacrolix/go-libutp
```

The import path is `github.com/anacrolix/go-libutp`; the package name is `utp`.

## Usage

`Socket` implements both `net.Listener` and `net.PacketConn`, and the connections it hands out
implement `net.Conn`, so µTP mostly drops into code written against TCP.

Dialling:

```go
s, err := utp.NewSocket("udp", ":0")
if err != nil {
	return err
}
defer s.Close()

c, err := s.DialContext(ctx, "udp", "example.com:4242")
if err != nil {
	return err
}
defer c.Close()

_, err = io.WriteString(c, "hello")
```

Accepting:

```go
s, err := utp.NewSocket("udp", ":4242")
if err != nil {
	return err
}
defer s.Close()

for {
	c, err := s.Accept()
	if err != nil {
		return err
	}
	go handle(c)
}
```

`Socket.Dial` and `Socket.DialTimeout` are also available for the simpler cases.

### Wrapping an existing PacketConn

`NewSocketFromPacketConn` runs µTP over a `net.PacketConn` you already have, which is useful when
the port is shared with another protocol or comes from elsewhere:

```go
pc, err := net.ListenPacket("udp", ":4242")
if err != nil {
	return err
}
s, err := utp.NewSocketFromPacketConn(pc)
```

### Sharing a port with non-µTP traffic

Packets that aren't µTP are not dropped: `Socket` implements `net.PacketConn`, and its `ReadFrom`
and `WriteTo` carry exactly those packets. That's how a single UDP port can serve µTP connections
and something else — a DHT, say — at the same time.

Two caveats on that `net.PacketConn`: non-µTP packets are buffered and dropped if the reader
doesn't keep up, and the deadline methods on `Socket` are unimplemented (they panic). Deadlines on
an accepted or dialled `Conn` work normally.

### Options

- `utp.WithLogger(l)` — pass to `NewSocket`/`NewSocketFromPacketConn` to give a socket its own
  `*slog.Logger`, instead of the package-level `utp.Logger`. That one is nil until you set it,
  which means sockets log to `slog.Default()`.
- `Socket.SetFirewallCallback` and `Socket.SetSyncFirewallCallback` — reject incoming connections
  before they're acknowledged, so the peer sees no response at all rather than an accept followed
  by a close. Prefer the synchronous variant; it's called under the package lock and is consulted
  for every incoming connection.
- `Socket.SetOption` — set the underlying libutp context options directly.

## Pure Go implementation

[`purego`](purego) is µTP implemented in Go, with no cgo and no C++ compiler. It's a port of the
same libutp sources vendored here: the state machine, the LEDBAT congestion controller, selective
acknowledgements and fast resend, the retransmission timers and the MTU search all follow the
reference implementation, down to the constants they're tuned with.

```go
import "github.com/anacrolix/go-libutp/purego"

s, err := purego.NewSocket("udp", ":4242")
```

It imports nothing outside the standard library, logging included, and it builds for every
platform Go targets. The API mirrors the one above — `Socket` is a `net.Listener` and a `net.PacketConn`, connections
are `net.Conn`, non-µTP packets come out of `Socket.ReadFrom` — so the two are mostly
interchangeable. It passes `golang.org/x/net/nettest`'s `TestConn` conformance suite, and the
[interop](interop) package tests it against libutp itself: in both directions, in both roles, and
over a link that drops, delays and duplicates packets.

Two of libutp's inputs aren't available portably from Go. ICMP fragmentation-needed reports aren't
fed back in, and the don't-fragment bit isn't set on MTU probes, so path MTU is inferred from
timeouts and duplicate acks rather than reported outright. Neither affects correctness; the search
just tends to settle high. The cgo package doesn't act on ICMP either.

## Picking one at build time

[`utp`](utp) is one interface over both, and selects one of them when you build. Depend on it
rather than on either implementation and the choice stays yours:

```go
import "github.com/anacrolix/go-libutp/utp"

s, err := utp.NewSocket("udp", ":4242")   // the implementation the build selected
c, err := s.DialContext(ctx, "", "example.com:4242")
```

`utp.Default` is libutp. The `purego` build tag, or cgo being off, makes it `utp.Purego` instead,
and the C++ sources are then not compiled at all — the tag and the package it selects share a name:

```sh
go build -tags purego ./...
CGO_ENABLED=0 go build ./...
```

`utp.NewSocket` and `utp.NewSocketFromPacketConn` have the same signatures as the constructors of
those names in both underlying packages, so moving a caller onto this package is an import change.

`utp.Socket` is an interface, so code can be handed one without caring which is underneath, and so
is `utp.Implementation` — `utp.Purego` and `utp.Libutp` are values of it, and `utp.Default` is
whichever the build picked. `utp.Libutp` only exists where libutp is being compiled. That's how the
interop tests run each implementation against the other, and each against itself.

An `Implementation` has one method: it makes a `Socket` over a `net.PacketConn`. Opening a port
isn't its job, so `utp.Listen` does that for it, and code holding a PacketConn already can hand it
straight over:

```go
s, err := utp.Listen(utp.Purego, "udp", ":4242")
s, err := utp.Purego.NewSocket(pc)
```

The tunables both implementations share are options: `utp.WithLogger`, `utp.WithBufferSizes` and
`utp.WithTargetDelay`, plus `Socket.SetLogging` for the protocol's own log categories. Anything an
implementation offers beyond that stays in its own package.

Two differences to know about. `utp.Listen` listens with `net.ListenPacket`, so it takes UDP
networks only — the cgo package's own `NewSocket` also understands the in-process test network, and
through `utp` you'd reach that by opening it yourself and calling `Implementation.NewSocket`. And
deadlines on the `Socket` itself, which only affect `ReadFrom` and `WriteTo`, aren't supported by
libutp: it returns an error wrapping `errors.ErrUnsupported`, where the pure implementation honours
them. Deadlines on connections work in both.

## ucat

`cmd/ucat` is a netcat-alike over µTP, handy for smoke-testing:

```sh
go run ./cmd/ucat -l :4242         # listen
go run ./cmd/ucat localhost:4242   # dial, then pipe stdin/stdout
```

## Development

The [justfile](justfile) mirrors the CI jobs, so what you run locally is what CI runs:

```sh
just test          # go test -race -count 2 ./...
just bench         # build/smoke the benchmarks
just test-purego   # the pure Go implementation selected via the build tag
just test-nocgo    # the pure Go packages with cgo off entirely
just asan          # tests under LeakSanitizer
```

`just asan` is clean on Linux and macOS; see [lsan_suppressions.txt](lsan_suppressions.txt) for the
macOS system-library allocations it has to ignore.

## Release history

See [CHANGELOG.md](CHANGELOG.md).

## License

MIT, inherited from libutp. See [LICENSE](LICENSE).
