//go:build cgo && !purego

package utp

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-quicktest/qt"
)

func TestDefaultImplementationIsLibutp(t *testing.T) {
	qt.Check(t, qt.Equals(fmt.Sprint(Default), "libutp"))
	qt.Check(t, qt.Equals(Default, Libutp))
}

// libutp's Socket has no deadlines. Reporting that beats the panic the underlying package raises.
func TestLibutpSocketDeadlinesUnsupported(t *testing.T) {
	s, err := Listen(Libutp, "udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	qt.Check(t, qt.ErrorIs(s.SetDeadline(time.Now()), errors.ErrUnsupported))
	qt.Check(t, qt.ErrorIs(s.SetReadDeadline(time.Now()), errors.ErrUnsupported))
	qt.Check(t, qt.ErrorIs(s.SetWriteDeadline(time.Now()), errors.ErrUnsupported))
}

// Both implementations are reachable by name when libutp is being compiled, which is what lets
// the interop tests run each against the other.
func TestBothImplementationsAvailable(t *testing.T) {
	for _, impl := range []Implementation{Libutp, Purego} {
		s, err := Listen(impl, "udp", "localhost:0")
		qt.Assert(t, qt.IsNil(err), qt.Commentf("%v", impl))
		qt.Check(t, qt.IsNil(s.Close()))
	}
	qt.Check(t, qt.Not(qt.Equals(Libutp, Purego)))
}
