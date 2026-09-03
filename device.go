package main

import (
	"image"
	"time"

	"github.com/muesli/streamdeck"
)

// Key represents a single button/key event coming from a deck device.
type Key struct {
	Index   uint8
	Pressed bool
}

// deviceBackend is implemented by every piece of hardware deckmaster can
// drive. Device wraps a backend and exposes the fields/methods the rest of
// the application uses, independent of the actual hardware underneath.
type deviceBackend interface {
	Open() error
	Close() error
	FirmwareVersion() (string, error)
	Reset() error
	Clear() error
	Sleep() error
	Wake() error
	Asleep() bool
	SetBrightness(percent uint8) error
	SetSleepFadeDuration(t time.Duration)
	SetSleepTimeout(t time.Duration)
	SetImage(index uint8, img image.Image) error
	ReadKeys() (chan Key, error)
}

// Device is a hardware-independent handle to a deck device, e.g. a genuine
// Elgato Stream Deck or a compatible third-party device such as the Ajazz
// AKP03E.
type Device struct {
	ID     string
	Serial string

	Columns uint8
	Rows    uint8
	Keys    uint8
	Pixels  uint
	DPI     uint
	Padding uint

	backend deviceBackend
}

// Open the device for input/output. This must be called before trying to
// communicate with the device.
func (d *Device) Open() error {
	return d.backend.Open()
}

// Close the connection with the device.
func (d *Device) Close() error {
	return d.backend.Close()
}

// FirmwareVersion returns the firmware version of the device.
func (d *Device) FirmwareVersion() (string, error) {
	return d.backend.FirmwareVersion()
}

// Reset the device, clearing all button images and showing the standby image.
func (d *Device) Reset() error {
	return d.backend.Reset()
}

// Clear the device, setting a black/blank image on all buttons.
func (d *Device) Clear() error {
	return d.backend.Clear()
}

// Sleep puts the device asleep, waiting for a key event to wake it up.
func (d *Device) Sleep() error {
	return d.backend.Sleep()
}

// Wake wakes the device from sleep.
func (d *Device) Wake() error {
	return d.backend.Wake()
}

// Asleep returns true if the device is asleep.
func (d *Device) Asleep() bool {
	return d.backend.Asleep()
}

// SetBrightness sets the background lighting brightness from 0 to 100 percent.
func (d *Device) SetBrightness(percent uint8) error {
	return d.backend.SetBrightness(percent)
}

// SetSleepFadeDuration sets the duration of the fading animation when the
// device is put to sleep or wakes up.
func (d *Device) SetSleepFadeDuration(t time.Duration) {
	d.backend.SetSleepFadeDuration(t)
}

// SetSleepTimeout sets the time after which the device will sleep if no key
// events are received.
func (d *Device) SetSleepTimeout(t time.Duration) {
	d.backend.SetSleepTimeout(t)
}

// SetImage sets the image of a button on the device. The provided image
// needs to be in the correct resolution for the device. The index starts
// with 0 being the top-left button.
func (d *Device) SetImage(index uint8, img image.Image) error {
	return d.backend.SetImage(index, img)
}

// ReadKeys returns a channel, which it will use to emit key presses/releases.
func (d *Device) ReadKeys() (chan Key, error) {
	return d.backend.ReadKeys()
}

// elgatoBackend adapts a genuine (github.com/muesli/streamdeck) Elgato
// Stream Deck to the deviceBackend interface.
type elgatoBackend struct {
	dev *streamdeck.Device
}

func (b *elgatoBackend) Open() error                          { return b.dev.Open() }
func (b *elgatoBackend) Close() error                         { return b.dev.Close() }
func (b *elgatoBackend) FirmwareVersion() (string, error)     { return b.dev.FirmwareVersion() }
func (b *elgatoBackend) Reset() error                         { return b.dev.Reset() }
func (b *elgatoBackend) Clear() error                         { return b.dev.Clear() }
func (b *elgatoBackend) Sleep() error                         { return b.dev.Sleep() }
func (b *elgatoBackend) Wake() error                          { return b.dev.Wake() }
func (b *elgatoBackend) Asleep() bool                         { return b.dev.Asleep() }
func (b *elgatoBackend) SetBrightness(percent uint8) error    { return b.dev.SetBrightness(percent) }
func (b *elgatoBackend) SetSleepFadeDuration(t time.Duration) { b.dev.SetSleepFadeDuration(t) }
func (b *elgatoBackend) SetSleepTimeout(t time.Duration)      { b.dev.SetSleepTimeout(t) }

func (b *elgatoBackend) SetImage(index uint8, img image.Image) error {
	return b.dev.SetImage(index, img)
}

func (b *elgatoBackend) ReadKeys() (chan Key, error) {
	sch, err := b.dev.ReadKeys()
	if err != nil {
		return nil, err
	}

	kch := make(chan Key)
	go func() {
		defer close(kch)
		for k := range sch {
			kch <- Key{Index: k.Index, Pressed: k.Pressed}
		}
	}()
	return kch, nil
}

// Devices returns every supported deck device currently connected: genuine
// Elgato Stream Decks as well as compatible third-party devices such as the
// Ajazz AKP03E.
func Devices() ([]Device, error) {
	var out []Device

	sd, err := streamdeck.Devices()
	if err != nil {
		return nil, err
	}
	for i := range sd {
		dev := sd[i]
		out = append(out, Device{
			ID:      dev.ID,
			Serial:  dev.Serial,
			Columns: dev.Columns,
			Rows:    dev.Rows,
			Keys:    dev.Keys,
			Pixels:  dev.Pixels,
			DPI:     dev.DPI,
			Padding: dev.Padding,
			backend: &elgatoBackend{dev: &dev},
		})
	}

	out = append(out, ajazzDevices()...)

	return out, nil
}
