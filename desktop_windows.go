//go:build windows

package main

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Desktop provides an interface to the Windows shell. Where the X11 port
// subscribes to PropertyNotify events on the root window, this one polls the
// foreground window: a SetWinEventHook based implementation would need a
// thread-locked message pump, and polling is cheap enough at these intervals.
type Desktop struct {
	mu sync.Mutex
	// handles maps the truncated window ID we hand out to the full HWND.
	// User32 handles are documented to fit in 32 bits, but we keep the
	// original around rather than relying on that when acting on a window.
	handles      map[uint32]windows.HWND
	activeWindow Window
	done         chan struct{}
	closeOnce    sync.Once
}

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	procGetForegroundWindow    = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW         = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW   = user32.NewProc("GetWindowTextLengthW")
	procGetWindowThreadProcess = user32.NewProc("GetWindowThreadProcessId")
	procSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procIsWindow               = user32.NewProc("IsWindow")
)

const wmClose = 0x0010

// maxPollInterval keeps the foreground window responsive even when the caller
// asks for a long timeout, which on X11 is only a screensaver fallback tick.
const maxPollInterval = 250 * time.Millisecond

// ConnectDesktop establishes a connection with the Windows shell.
func ConnectDesktop() (*Desktop, error) {
	if err := user32.Load(); err != nil {
		return nil, err
	}
	return &Desktop{
		handles: make(map[uint32]windows.HWND),
		done:    make(chan struct{}),
	}, nil
}

// Close terminates the connection.
func (x *Desktop) Close() {
	if x == nil {
		return
	}
	x.closeOnce.Do(func() { close(x.done) })
}

// TrackWindows monitors the active window.
func (x *Desktop) TrackWindows(ch chan interface{}, timeout time.Duration) {
	if x == nil {
		return
	}

	interval := timeout
	if interval <= 0 || interval > maxPollInterval {
		interval = maxPollInterval
	}

	if win, ok := x.window(); ok {
		x.setActive(win)
		if ch != nil {
			go func() { ch <- ActiveWindowChangedEvent{Window: win} }()
		}
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-x.done:
				return
			case <-ticker.C:
				if win, ok := x.window(); ok && win.ID != x.ActiveWindow().ID {
					x.setActive(win)
					if ch != nil {
						ch <- ActiveWindowChangedEvent{Window: win}
					}
				}
				for _, closed := range x.reapClosedWindows() {
					if ch != nil {
						ch <- WindowClosedEvent{Window: Window{ID: closed}}
					}
				}
			}
		}
	}()
}

// ActiveWindow returns the currently active window.
func (x *Desktop) ActiveWindow() Window {
	if x == nil {
		return Window{}
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.activeWindow
}

// RequestActivation requests a window to be focused.
func (x *Desktop) RequestActivation(w Window) error {
	hwnd, ok := x.handle(w.ID)
	if !ok {
		return errors.New("unknown window")
	}
	// SetForegroundWindow returns false when the foreground lock prevents the
	// change, which is a normal outcome rather than a call failure.
	if r, _, _ := procSetForegroundWindow.Call(uintptr(hwnd)); r == 0 {
		return errors.New("the shell refused the foreground change")
	}
	return nil
}

// CloseWindow closes a window.
func (x *Desktop) CloseWindow(w Window) error {
	hwnd, ok := x.handle(w.ID)
	if !ok {
		return errors.New("unknown window")
	}
	r, _, err := procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0)
	if r == 0 {
		return err
	}
	return nil
}

func (x *Desktop) setActive(w Window) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.activeWindow = w
}

func (x *Desktop) handle(id uint32) (windows.HWND, bool) {
	if x == nil {
		return 0, false
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	hwnd, ok := x.handles[id]
	return hwnd, ok
}

// reapClosedWindows drops handles that are no longer live windows and returns
// their IDs so the caller can emit WindowClosedEvents for them.
func (x *Desktop) reapClosedWindows() []uint32 {
	x.mu.Lock()
	defer x.mu.Unlock()

	var closed []uint32
	for id, hwnd := range x.handles {
		if r, _, _ := procIsWindow.Call(uintptr(hwnd)); r == 0 {
			delete(x.handles, id)
			closed = append(closed, id)
		}
	}
	return closed
}

// window inspects the current foreground window. It reports false for windows
// we cannot name, matching the X11 port, which skips windows without a
// WM_CLASS or WM_NAME.
func (x *Desktop) window() (Window, bool) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return Window{}, false
	}

	name := windowText(hwnd)
	if name == "" {
		return Window{}, false
	}

	class, err := windowProcessName(hwnd)
	if err != nil {
		return Window{}, false
	}

	//nolint:gosec // user32 handles are documented to fit in 32 bits
	id := uint32(hwnd)

	x.mu.Lock()
	x.handles[id] = windows.HWND(hwnd)
	x.mu.Unlock()

	return Window{
		ID:    id,
		Class: class,
		Name:  name,
		// Extracting the window icon means reading HICON via WM_GETICON and
		// converting it through GetIconInfo/GetDIBits. Until that exists,
		// recentWindow widgets fall back to their label.
		Icon: nil,
	}, true
}

func windowText(hwnd uintptr) string {
	length, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if length == 0 {
		return ""
	}
	buf := make([]uint16, length+1)
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), length+1)
	if n == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

// windowProcessName returns the base name of the executable owning a window,
// without its extension. It stands in for X11's WM_CLASS: "firefox.exe"
// becomes "firefox", so deck configs can match on it the same way.
func windowProcessName(hwnd uintptr) (string, error) {
	var pid uint32
	procGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid))) //nolint:errcheck
	if pid == 0 {
		return "", errors.New("no owning process")
	}

	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h) //nolint:errcheck

	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", err
	}

	base := filepath.Base(windows.UTF16ToString(buf[:size]))
	return strings.TrimSuffix(base, filepath.Ext(base)), nil
}
