package main

import (
	"fmt"
	"image"
	"os"
	"time"
)

// reconnectInterval limits how often a widget retries a PulseAudio connection
// that is down, so a missing daemon doesn't cause a connect attempt per frame.
const reconnectInterval = 10 * time.Second

// MicrophoneMuteWidget is a widget displaying if the default microphone is muted.
type MicrophoneMuteWidget struct {
	*ButtonWidget
	pulse      *PulseAudioClient
	lastRetry  time.Time
	mute       bool
	iconUnmute image.Image
	iconMute   image.Image
}

// source returns the current default source, connecting to PulseAudio on demand.
// It reports a nil source (without an error) while the daemon is unavailable, so
// callers can keep rendering the unmuted state instead of failing.
func (w *MicrophoneMuteWidget) source() *Source {
	if w.pulse == nil {
		if time.Since(w.lastRetry) < reconnectInterval {
			return nil
		}
		w.lastRetry = time.Now()

		pulse, err := NewPulseAudioClient()
		if err != nil {
			verboseLog("microphone widget: PulseAudio still unavailable: %s", err)
			return nil
		}
		w.pulse = pulse
	}

	source, err := w.pulse.DefaultSource()
	if err != nil {
		// The connection is unusable, drop it so we reconnect later.
		errorLog(err, "failed to get PulseAudio default source")
		_ = w.pulse.Close()
		w.pulse = nil
		w.lastRetry = time.Now()
		return nil
	}
	return source
}

// NewMicrophoneMuteWidget returns a new MicrophoneMuteWidget.
func NewMicrophoneMuteWidget(bw *BaseWidget, opts WidgetConfig) (*MicrophoneMuteWidget, error) {
	widget, err := NewButtonWidget(bw, opts)
	if err != nil {
		return nil, err
	}

	var iconUnmutePath, iconMutePath string
	_ = ConfigValue(opts.Config["icon"], &iconUnmutePath)
	_ = ConfigValue(opts.Config["iconMute"], &iconMutePath)
	iconUnmute, err := preloadImage(widget.base, iconUnmutePath)
	if err != nil {
		return nil, err
	}
	iconMute, err := preloadImage(widget.base, iconMutePath)
	if err != nil {
		return nil, err
	}

	w := &MicrophoneMuteWidget{
		ButtonWidget: widget,
		iconUnmute:   iconUnmute,
		iconMute:     iconMute,
	}

	// A missing PulseAudio daemon must not take down the whole deck: the widget
	// stays loaded in its unmuted state and keeps retrying the connection.
	if source := w.source(); source != nil {
		w.mute = source.Mute
	}

	return w, nil
}

// RequiresUpdate returns true when the widget wants to be repainted.
func (w *MicrophoneMuteWidget) RequiresUpdate() bool {
	source := w.source()
	if source == nil {
		return w.BaseWidget.RequiresUpdate()
	}

	return w.mute != source.Mute || w.BaseWidget.RequiresUpdate()
}

// Update renders the widget.
func (w *MicrophoneMuteWidget) Update() error {
	source := w.source()
	if source != nil && w.mute != source.Mute {
		w.mute = source.Mute

		if w.mute {
			w.SetImage(w.iconMute)
		} else {
			w.SetImage(w.iconUnmute)
		}
	}

	return w.ButtonWidget.Update()
}

// TriggerAction gets called when a button is pressed.
func (w *MicrophoneMuteWidget) TriggerAction(_ bool) {
	source := w.source()
	if source == nil {
		fmt.Fprintln(os.Stderr, "Can't get pulseaudio default source: PulseAudio is unavailable")
		return
	}
	if err := w.pulse.SetSourceMute(source, !source.Mute); err != nil {
		fmt.Fprintf(os.Stderr, "Can't set pulseaudio default source mute: %s\n", err)
		return
	}
}

// Close gets called when a button is unloaded.
func (w *MicrophoneMuteWidget) Close() error {
	if w.pulse == nil {
		return nil
	}
	err := w.pulse.Close()
	w.pulse = nil
	return err
}

func preloadImage(base string, path string) (image.Image, error) {
	path, err := expandPath(base, path)
	if err != nil {
		return nil, err
	}
	icon, err := loadImage(path)
	if err != nil {
		return nil, err
	}

	return icon, nil
}
