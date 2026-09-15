//go:build !linux

package main

import "errors"

// umbriel only runs on Linux, so the widget and the action it backs fail at
// construction here rather than drawing an empty key or silently doing
// nothing. See umbriel_linux.go for what they do where they work.

var errUmbrielUnsupported = errors.New("umbriel is only supported on Linux")

// NewUmbrielWidget reports that this platform has no umbriel to talk to.
func NewUmbrielWidget(_ *BaseWidget, _ WidgetConfig) (Widget, error) {
	return nil, errUmbrielUnsupported
}

// umbrielCommand reports that this platform has no umbriel to talk to.
func umbrielCommand(_ string) error {
	return errUmbrielUnsupported
}
