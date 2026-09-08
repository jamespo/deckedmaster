package main

import (
	"strconv"
	"strings"
)

// formatKeycodes resolves a key name to the numeric code of whichever keycodes
// table this platform was built with. Names are the portable spelling and
// should be preferred in deck configs; anything the platform has no key for is
// returned unchanged, and reported as an invalid keycode when it is used.
func formatKeycodes(keycode string) string {
	for k, v := range keycodes {
		if strings.EqualFold(keycode, k) {
			keycode = strconv.Itoa(int(v))
			break
		}
	}

	return keycode
}
