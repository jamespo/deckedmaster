//go:build linux

package main

import (
	"bytes"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// evdev key codes of the modifier keys (see linux/input-event-codes.h).
var evdevModifiers = []struct {
	code uint16
	mod  Modifiers
}{
	{29, ModCtrl},   // KEY_LEFTCTRL
	{97, ModCtrl},   // KEY_RIGHTCTRL
	{42, ModShift},  // KEY_LEFTSHIFT
	{54, ModShift},  // KEY_RIGHTSHIFT
	{56, ModAlt},    // KEY_LEFTALT
	{100, ModAlt},   // KEY_RIGHTALT
	{125, ModSuper}, // KEY_LEFTMETA
	{126, ModSuper}, // KEY_RIGHTMETA
}

const (
	evdevKeyMax = 0x2ff // KEY_MAX
	iocRead     = 2
)

// evdevIOC builds an evdev ioctl request number, like the kernel's _IOC macro.
func evdevIOC(nr, size uintptr) uintptr {
	return iocRead<<30 | size<<16 | uintptr('E')<<8 | nr
}

// evdevDevices keeps the input devices modifiers are read from open between
// queries: closing an evdev node waits for an RCU grace period (tens of
// milliseconds each), which would add up to a noticeable delay per key press
// on a machine with a couple of dozen input devices. Opening one is cheap, so
// the device list is simply rescanned on every query to pick up hotplugs.
var evdevDevices = struct {
	sync.Mutex
	fds map[string]int
}{fds: map[string]int{}}

var modifierPermissionOnce sync.Once

// heldModifiers reports which modifier keys are currently held down on any
// keyboard. It asks the kernel for each input device's key state directly
// (EVIOCGKEY), so it works the same under X11, any Wayland compositor, behind
// remappers like keyd and on a bare console. Reading /dev/input/event* needs
// the same permissions as the virtual keyboard, usually membership of the
// "input" group.
func heldModifiers() Modifiers {
	paths, _ := filepath.Glob("/dev/input/event*")

	evdevDevices.Lock()
	defer evdevDevices.Unlock()
	fds := evdevDevices.fds

	present := make(map[string]bool, len(paths))
	var held Modifiers
	readable := 0
	for _, path := range paths {
		present[path] = true

		var m Modifiers
		ok := false
		if fd, open := fds[path]; open {
			if m, ok = deviceModifiers(fd); !ok {
				// unplugged (or replaced by another device), try a fresh handle
				_ = unix.Close(fd)
				delete(fds, path)
			}
		}
		if _, open := fds[path]; !open {
			fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
			if err != nil {
				continue
			}
			if m, ok = deviceModifiers(fd); !ok {
				_ = unix.Close(fd)
				continue
			}
			fds[path] = fd
		}
		readable++
		held |= m
	}

	// forget devices that have gone away
	for path, fd := range fds {
		if !present[path] {
			_ = unix.Close(fd)
			delete(fds, path)
		}
	}

	if readable == 0 && len(paths) > 0 {
		modifierPermissionOnce.Do(func() {
			errorLogF("can't read any of /dev/input/event* - modifier actions are disabled. " +
				"Add your user to the 'input' group to enable them.")
		})
	}
	return held
}

// deviceModifiers returns the modifiers held on a single open input device.
// Our own virtual keyboard reports none: anything it holds down is an emulated
// key press, not the user holding a modifier.
func deviceModifiers(fd int) (Modifiers, bool) {
	name := make([]byte, 256)
	if err := ioctl(fd, evdevIOC(0x06, uintptr(len(name))), unsafe.Pointer(&name[0])); err != nil { // EVIOCGNAME
		return 0, false
	}
	if n := bytes.IndexByte(name, 0); n >= 0 && string(name[:n]) == "deckedmaster" {
		return 0, true
	}

	state := make([]byte, evdevKeyMax/8+1)
	if err := ioctl(fd, evdevIOC(0x18, uintptr(len(state))), unsafe.Pointer(&state[0])); err != nil { // EVIOCGKEY
		return 0, false
	}

	var held Modifiers
	for _, m := range evdevModifiers {
		if state[m.code/8]&(1<<(m.code%8)) != 0 {
			held |= m.mod
		}
	}
	return held, true
}

func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}
