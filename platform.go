package main

import "image"

// This file defines the platform port: the types and functions every supported
// operating system has to provide. Each port lives in a build-tagged file pair,
// one per concern:
//
//	desktop_linux.go   / desktop_windows.go    active window tracking
//	keyboard_linux.go  / keyboard_windows.go   virtual keyboard
//	keycodes_linux.go  / keycodes_windows.go   key name -> scancode table
//	process_linux.go   / process_windows.go    child process containment
//	signals_linux.go   / signals_windows.go    process signals
//	shell_linux.go     / shell_windows.go      shell used by command widgets
//
// A port that cannot support a feature is expected to fail at construction
// rather than to panic later: run() logs the error, leaves the corresponding
// global nil, and carries on with that feature's widgets disabled. Methods on
// port types must therefore tolerate a nil receiver, the way *PulseAudio
// already does.

// Window describes a window belonging to another application.
type Window struct {
	ID    uint32
	Class string
	Name  string
	Icon  image.Image
}

// ActiveWindowChangedEvent gets emitted when the active window changes.
type ActiveWindowChangedEvent struct {
	Window Window
}

// WindowClosedEvent gets emitted when a window gets closed.
type WindowClosedEvent struct {
	Window Window
}

// VirtualKeyboard emulates key presses for the "keycode" deck action. The
// codes it accepts are whatever the platform's keycodes table produces, so
// they are only meaningful to the port that created the keyboard.
type VirtualKeyboard interface {
	KeyDown(key int) error
	KeyUp(key int) error
	KeyPress(key int) error
	Close() error
}
