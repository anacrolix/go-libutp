package purego

import (
	"log/slog"
	"testing"

	"github.com/go-quicktest/qt"
)

// A Socket logs to the logger it was given, and falls back to the package Logger and then slog's
// default when it wasn't given one.
func TestSocketLogger(t *testing.T) {
	l := slog.New(slog.DiscardHandler)
	s, err := NewSocket("udp", "localhost:0", WithLogger(l))
	qt.Assert(t, qt.IsNil(err))
	defer s.Close()
	qt.Check(t, qt.Equals(s.logger, l))

	d, err := NewSocket("udp", "localhost:0")
	qt.Assert(t, qt.IsNil(err))
	defer d.Close()
	// Nothing here sets the package Logger, so this is slog's default. A nil one would panic on
	// the first thing worth logging.
	qt.Check(t, qt.IsNotNil(d.logger))
}

// The categories a Socket logs under appear by name.
func TestLogLevelNames(t *testing.T) {
	qt.Check(t, qt.Equals(logNormal.String(), "normal"))
	qt.Check(t, qt.Equals(logMTU.String(), "mtu"))
	qt.Check(t, qt.Equals(logDebug.String(), "debug"))
}
