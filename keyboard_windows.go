//go:build windows

package main

import (
	"errors"
	"time"

	"golang.org/x/sys/windows"
)

// keybd_event is documented as superseded by SendInput, but it is still
// supported and is implemented on top of SendInput internally. It takes plain
// scalar arguments, which avoids having to reproduce the INPUT union's C
// layout across amd64, 386 and arm64 by hand for what this needs to do.
var procKeybdEvent = windows.NewLazySystemDLL("user32.dll").NewProc("keybd_event")

const (
	keyEventFExtendedKey = 0x0001
	keyEventFKeyUp       = 0x0002

	// keyPressDuration gives applications a press to observe. Sending the up
	// event in the same tick as the down event makes some of them drop it.
	keyPressDuration = 12 * time.Millisecond
)

// extendedKeys are the virtual keys that must carry KEYEVENTF_EXTENDEDKEY, so
// that applications distinguish them from their numeric keypad counterparts.
var extendedKeys = map[int]bool{
	0x21: true, // Pageup
	0x22: true, // Pagedown
	0x23: true, // End
	0x24: true, // Home
	0x25: true, // Left
	0x26: true, // Up
	0x27: true, // Right
	0x28: true, // Down
	0x2C: true, // Sysrq / PrintScreen
	0x2D: true, // Insert
	0x2E: true, // Delete
	0x5B: true, // Leftmeta
	0x5C: true, // Rightmeta
	0x5D: true, // Menu / Compose
	0x6F: true, // Kpslash
	0x90: true, // Numlock
	0xA3: true, // Rightctrl
	0xA5: true, // Rightalt
}

type windowsKeyboard struct{}

// newVirtualKeyboard creates a virtual keyboard driven by user32. The codes it
// accepts are Windows virtual-key codes, which is what keycodes_windows.go
// produces.
func newVirtualKeyboard() (VirtualKeyboard, error) {
	if err := procKeybdEvent.Find(); err != nil {
		return nil, err
	}
	return &windowsKeyboard{}, nil
}

func (k *windowsKeyboard) KeyDown(key int) error {
	return k.send(key, 0)
}

func (k *windowsKeyboard) KeyUp(key int) error {
	return k.send(key, keyEventFKeyUp)
}

func (k *windowsKeyboard) KeyPress(key int) error {
	if err := k.KeyDown(key); err != nil {
		return err
	}
	time.Sleep(keyPressDuration)
	return k.KeyUp(key)
}

// Close is a no-op: keybd_event holds no per-keyboard state to release.
func (k *windowsKeyboard) Close() error { return nil }

func (k *windowsKeyboard) send(key int, flags uint32) error {
	if key <= 0 || key > 0xFF {
		return errors.New("virtual-key code out of range")
	}
	if extendedKeys[key] {
		flags |= keyEventFExtendedKey
	}
	// keybd_event returns void, so there is no result to check. A scan code of
	// 0 tells the system to derive one from the virtual-key code.
	procKeybdEvent.Call(uintptr(key), 0, uintptr(flags), 0) //nolint:errcheck
	return nil
}
