//go:build lsan
// +build lsan

package utp

import (
	"log"
	"os"
	"testing"

	"github.com/anacrolix/lsan"
)

// Run the leak-sanitized tests with:
//
//	ASAN_OPTIONS=detect_leaks=1 \
//	  LSAN_OPTIONS=suppressions=$PWD/lsan_suppressions.txt \
//	  go test -tags 'lsan netgo' ./...
//
// The netgo tag and the suppressions file only matter on macOS, where cgo pulls
// in libSystem one-time initializations that LeakSanitizer reports as leaks; see
// lsan_suppressions.txt. Neither affects Linux, where none of those leaks occur.
func TestMain(m *testing.M) {
	log.Printf("lsan main running")
	//lsan.LeakABit()
	code := m.Run()
	lsan.LsanDoLeakCheck()
	os.Exit(code)
}
