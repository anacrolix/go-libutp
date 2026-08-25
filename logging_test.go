package utp

import (
	"log/slog"
	"testing"
)

// A Socket logs to the logger it was given, and gets one of its own when it wasn't given any.
func TestSocketLogger(t *testing.T) {
	l := slog.New(slog.DiscardHandler)
	s, err := NewSocket("udp", "localhost:0", WithLogger(l))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.logger != l {
		t.Error("socket didn't take the logger it was given")
	}

	d, err := NewSocket("udp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// Nothing here sets the package Logger, so this is slog's default. A nil one would panic on
	// the first thing worth logging.
	if d.logger == nil {
		t.Error("socket without WithLogger got no logger")
	}
}
