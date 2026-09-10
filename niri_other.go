//go:build !linux

package main

import "errors"

// niri only runs on Linux, so the widget and the action it backs fail at
// construction here rather than drawing an empty key or silently doing
// nothing. See niri_linux.go for what they do where they work.

var errNiriUnsupported = errors.New("niri is only supported on Linux")

// NewNiriWidget reports that this platform has no niri to talk to.
func NewNiriWidget(_ *BaseWidget, _ WidgetConfig) (Widget, error) {
	return nil, errNiriUnsupported
}

// niriCommand reports that this platform has no niri to talk to.
func niriCommand(_ string) error {
	return errNiriUnsupported
}
