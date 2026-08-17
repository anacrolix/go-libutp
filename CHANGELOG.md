# Changelog

## Unreleased

- Clamp the peer-supplied uTP delay sample before it reaches the average-delay
  accumulator. A crafted `reply_micro` exactly half the 32-bit space from the
  baseline previously produced a sample of `-2^31` (or `+2^31-1` on the other
  branch) that tripped the `current_delay_sum` assertion and aborted the whole
  process. Reported against Erigon (erigontech/security#78); the same
  arithmetic is present in upstream bittorrent/libutp.
- Repair the MTU floor/ceiling range instead of asserting on it. A dropped MTU
  probe in steady state, or an interface/ICMP-reported MTU below the search
  floor, could leave `mtu_floor > mtu_ceiling` and abort on
  `assert(mtu_floor <= mtu_ceiling)` (bittorrent/libutp #105, #130). Both fixes
  mirror the corresponding handling in libtorrent's uTP implementation.

## v1.4.0 — 2025-11-21

- Fix unsynchronized event checks and signalling in `waitForConnect`
- Check for port overflow in inproc packetconn
- Make `utp_types.h` safer and compatible with cgo/Go builds (include `<stdint.h>`/`<stdbool.h>`, avoid retypedefs, normalize macros)
- Upgrade `golang.org/x/net` for latest nettest compatibility
- Reduce CI flakiness: skip known-bad test, run nettests sequentially

## v1.3.2 — 2024-09-10

- Upgrade to mmsg v1.0.1
- Fix: `bool`, `true`, and `false` are keywords in modern C
- Run `gorond`
- Bump `golang.org/x/net` from 0.7.0 to 0.17.0

## v1.3.1 — 2023-07-19

- Fix build errors in Go 1.21
- Switch CI to GitHub Actions

## v1.3.0 — 2023-03-04

- Bump `golang.org/x/net` from 0.0.0-20180524181706 to 0.7.0
- Move `ucat` command to `cmd/` directory
- Lower send callback error logging to debug level

## v1.2.0 — 2022-01-31

- Add optional function pattern for `NewSocket`
- Configure logging on a per-socket basis

## v1.1.0 — 2021-11-26

- Add synchronous firewall callback
- Run tests with leak sanitizer

## v1.0.x

### v1.0.5 — 2021-11-26

- Fix memory leak when connect fails to resolve
- Fix race in `sendtoCallback`; add race test
- Reduce `net.UDPAddr` allocations in `sendtoCallback`
- Reduce address allocations in `Socket.utpProcessUdp`
- Minimize time spent holding lock in `NewSocket`
- Run `utp_issue_deferred_acks` and `utp_check_timeouts` using timers
- Merge duplicate timeout checkers
- Tidy up error types
- CI: check for races in benchmarks

### v1.0.0 — 2019-04-26

- Initial release
