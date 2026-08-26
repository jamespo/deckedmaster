package main

import (
	"errors"
	"testing"
	"time"
)

// TestNilPulseAudioIsInert covers the headless/no-daemon case: when
// NewPulseAudio fails, the global pa stays nil and every call on it must
// degrade instead of panicking.
func TestNilPulseAudioIsInert(t *testing.T) {
	var nilPA *PulseAudio

	if got := nilPA.Updates(); got != nil {
		t.Error("Updates() on a nil PulseAudio should return a nil channel")
	}
	if nilPA.Muted(true) || nilPA.Muted(false) {
		t.Error("Muted() on a nil PulseAudio should report false")
	}
	if got := nilPA.CurrentSinkName(); got != "" {
		t.Errorf("CurrentSinkName() = %q, want empty", got)
	}
	if got := nilPA.CurrentSourceName(); got != "" {
		t.Errorf("CurrentSourceName() = %q, want empty", got)
	}
	for _, err := range []error{
		nilPA.ToggleMute(true),
		nilPA.SetSink("any"),
		nilPA.SetSource("any"),
	} {
		if !errors.Is(err, errNoPulseAudio) {
			t.Errorf("got %v, want errNoPulseAudio", err)
		}
	}

	nilPA.Close()

	// Start is run in its own goroutine by eventLoop and must return at once.
	done := make(chan struct{})
	go func() {
		nilPA.Start()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Start() on a nil PulseAudio did not return")
	}
}

// TestNilMediaPlayersIsInert covers a missing DBus session bus.
func TestNilMediaPlayersIsInert(t *testing.T) {
	var nilMP *MediaPlayers

	nilMP.Run()
	nilMP.SelectPlayer(1)
	if got := nilMP.ActivePlayer(); got != nil {
		t.Error("ActivePlayer() on a nil MediaPlayers should return nil")
	}
	if got := nilMP.ActivePlayerName(); got != "" {
		t.Errorf("ActivePlayerName() = %q, want empty", got)
	}
	if err := nilMP.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}
