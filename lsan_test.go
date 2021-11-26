//go:build lsan
// +build lsan

package utp

import (
	"log"
	"os"
	"testing"

	"github.com/anacrolix/lsan"
)

// Use this with ASAN_OPTIONS=detect_leaks=1 go test -tags lsan.
func TestMain(m *testing.M) {
	log.Printf("lsan main running")
	lsan.LeakABit()
	code := m.Run()
	lsan.LsanDoLeakCheck()
	os.Exit(code)
}
