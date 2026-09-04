# just defaults to `sh`, which isn't reliably on PATH on Windows CI runners;
# bash always is.
set windows-shell := ["bash", "-cu"]

default:
    @just --list

# Unit tests, as in the CI `test` job.
test:
    go test -race -count 2 ./...

# Benchmarks build/smoke, as in the CI `test` job (the '@' matches no tests).
bench:
    go test -bench . -run '@' ./...

# Unit tests with the pure Go implementation selected, as in the CI `purego` job. Only ./utp
# changes behaviour under the tag; the interop tests need libutp and exclude themselves.
test-purego:
    go test -race -count 2 -tags purego ./utp/...

# The pure Go packages have to build and pass with cgo off entirely, as in the CI `purego` job.
# -race needs cgo, so it can't be used here, and the libutp wrapper can't build at all.
test-nocgo:
    CGO_ENABLED=0 go test -count 2 ./utp/... ./purego/...

# netgo and the suppressions file only matter on macOS; both are inert on Linux.
# See lsan_suppressions.txt.
# Leak-sanitized tests, as in the CI `asan` job.
asan:
    ASAN_OPTIONS=detect_leaks=1 LSAN_OPTIONS=suppressions={{ justfile_directory() }}/lsan_suppressions.txt go test -tags 'lsan netgo' ./...
