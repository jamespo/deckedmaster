//go:build linux

package main

import "github.com/bendahl/uinput"

// uinputDevice is the path of the kernel's virtual input device. Creating a
// keyboard on it needs write permission, usually granted through a udev rule
// or by adding the user to the "input" group.
const uinputDevice = "/dev/uinput"

// newVirtualKeyboard creates a virtual keyboard through uinput. The codes it
// accepts are Linux evdev key codes, which is what keycodes_linux.go produces.
func newVirtualKeyboard() (VirtualKeyboard, error) {
	kb, err := uinput.CreateKeyboard(uinputDevice, []byte("deckedmaster"))
	if err != nil {
		return nil, err
	}
	return kb, nil
}
