//go:build windows

package main

import (
	"fmt"
	"os"

	"github.com/karalabe/hid"
)

// checkHIDSupport aborts if this binary was built with CGO_ENABLED=0. Without
// cgo, github.com/karalabe/hid silently compiles to its disabled stub instead
// of the real hidapi bindings: the binary still runs, but Devices() always
// returns none, so deckedmaster can never find a Stream Deck. That failure
// mode is silent otherwise, so warn loudly instead of just logging "no
// devices found" like a normal empty-USB-bus case.
func checkHIDSupport() {
	if hid.Supported() {
		return
	}
	fmt.Fprintln(os.Stderr, "ERROR: this build of deckedmaster was compiled with CGO_ENABLED=0 and cannot detect any devices.")
	fmt.Fprintln(os.Stderr, "Rebuild with CGO_ENABLED=1 and a mingw-w64 C compiler on PATH, e.g.:")
	fmt.Fprintln(os.Stderr, "  CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go build .")
	os.Exit(1)
}
