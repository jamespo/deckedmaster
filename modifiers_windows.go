//go:build windows

package main

import "golang.org/x/sys/windows"

var procGetAsyncKeyState = windows.NewLazySystemDLL("user32.dll").NewProc("GetAsyncKeyState")

// virtual-key codes of the modifier keys. VK_SHIFT, VK_CONTROL and VK_MENU
// cover both the left and right keys.
var vkModifiers = []struct {
	vk  uintptr
	mod Modifiers
}{
	{0x10, ModShift}, // VK_SHIFT
	{0x11, ModCtrl},  // VK_CONTROL
	{0x12, ModAlt},   // VK_MENU
	{0x5B, ModSuper}, // VK_LWIN
	{0x5C, ModSuper}, // VK_RWIN
}

// heldModifiers reports which modifier keys are currently held down.
func heldModifiers() Modifiers {
	if procGetAsyncKeyState.Find() != nil {
		return 0
	}

	var held Modifiers
	for _, m := range vkModifiers {
		state, _, _ := procGetAsyncKeyState.Call(m.vk) //nolint:errcheck
		// the most significant bit of the SHORT result is set while the key is down
		if state&0x8000 != 0 {
			held |= m.mod
		}
	}
	return held
}
