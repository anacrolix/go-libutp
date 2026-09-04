//go:build !cgo || purego

package utp

import (
	"fmt"
	"testing"
	"time"

	"github.com/go-quicktest/qt"
)

func TestDefaultImplementationIsPurego(t *testing.T) {
	qt.Check(t, qt.Equals(fmt.Sprint(Default), "purego"))
	qt.Check(t, qt.Equals(Default, Purego))
}

// The pure implementation does support Socket deadlines, so the default Socket does here.
func TestPuregoSocketDeadlinesWork(t *testing.T) {
	s, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	qt.Check(t, qt.IsNil(s.SetDeadline(time.Now().Add(time.Hour))))
	qt.Assert(t, qt.IsNil(s.SetReadDeadline(time.Now().Add(50*time.Millisecond))))
	_, _, err = s.ReadFrom(make([]byte, 1))
	qt.Check(t, qt.IsNotNil(err))
}
