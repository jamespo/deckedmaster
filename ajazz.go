package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/karalabe/hid"
)

// This implements the (reverse engineered, undocumented) HID protocol shared
// by Ajazz/Mirabox "Stream Dock" style devices, as used by the Ajazz AKP03E.
// It was derived from two independently written open source implementations
// that were each validated against real hardware:
//   - https://github.com/4ndv/mirajazz (Rust; the AKP03Erev2 profile matches
//     the VID/PID of the device this was developed against)
//   - https://github.com/bitfocus/companion-surface-mirabox-stream-dock (TS)
//
// Unlike a genuine Elgato Stream Deck (which talks HID feature reports),
// these devices are driven by sending short ASCII-ish "CRT" commands as
// regular HID output reports.

const (
	ajazzVendorID = 0x0300

	// ajazzInterface is the USB interface number of the vendor-specific HID
	// interface that speaks the "CRT" command protocol. The AKP03E also
	// exposes a second, unrelated boot-keyboard interface (interface 1) that
	// must be ignored.
	ajazzInterface = 0

	ajazzReadSize  = 512
	ajazzFadeDelay = time.Second / 30
)

// ajazzProfile describes the geometry/quirks of one AKP03E hardware revision.
type ajazzProfile struct {
	name       string
	packetSize int  // HID output report payload size, in bytes
	imageSize  int  // width/height (in pixels) of the JPEG image each key expects
	rotate     bool // whether the image needs a 90° clockwise rotation

	// bothKeyStates is true for devices that reliably report both press and
	// release events. Older firmware only ever reports "pressed", so a click
	// has to be synthesized as an immediate down+up.
	bothKeyStates bool

	rows, columns uint8
	keys          uint8 // addressable keys, including the 3 encoder presses
}

// ajazzProfiles maps USB product IDs to their device profile. Both known
// hardware revisions of the AKP03E are listed; the device this was built
// against reports VID 0x0300, PID 0x3002 ("rev. 2").
var ajazzProfiles = map[uint16]ajazzProfile{
	0x1002: {
		name: "Ajazz AKP03E", packetSize: 1024, imageSize: 60,
		rotate: false, bothKeyStates: false,
		rows: 3, columns: 3, keys: 12,
	},
	0x3002: {
		name: "Ajazz AKP03E (rev. 2)", packetSize: 1024, imageSize: 64,
		rotate: true, bothKeyStates: true,
		rows: 3, columns: 3, keys: 12,
	},
}

// ajazzDevices returns every connected, recognized Ajazz AKP03E.
func ajazzDevices() []Device {
	var out []Device

	for pid, profile := range ajazzProfiles {
		for _, info := range hid.Enumerate(ajazzVendorID, pid) {
			if info.Interface != ajazzInterface {
				// The other interface is a boot-keyboard interface unrelated
				// to the "CRT" command protocol.
				continue
			}

			out = append(out, Device{
				ID:      info.Path,
				Serial:  info.Serial,
				Columns: profile.columns,
				Rows:    profile.rows,
				Keys:    profile.keys,
				Pixels:  uint(profile.imageSize),
				DPI:     ajazzDPI(profile.imageSize),
				Padding: 0,
				backend: &ajazzDevice{
					info:       info,
					profile:    profile,
					brightness: 100,
				},
			})
		}
	}

	return out
}

// ajazzDPI approximates a DPI value scaled to the device's small key
// displays, used only to size rendered text/fonts sensibly.
func ajazzDPI(pixels int) uint {
	return uint(float64(pixels) * 1.85)
}

// ajazzDevice implements deviceBackend for the Ajazz AKP03E family.
type ajazzDevice struct {
	info    hid.DeviceInfo
	profile ajazzProfile

	mu  sync.Mutex
	dev *hid.Device

	brightness         uint8
	preSleepBrightness uint8
	asleep             bool

	sleepMutex   sync.RWMutex
	lastAction   time.Time
	sleepCancel  context.CancelFunc
	fadeDuration time.Duration
}

// Open connects to the device and runs its required init handshake.
func (a *ajazzDevice) Open() error {
	dev, err := a.info.Open()
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.dev = dev
	a.mu.Unlock()

	a.sleepMutex.Lock()
	a.lastAction = time.Now()
	a.sleepMutex.Unlock()

	return a.initialize()
}

// initialize sends the two commands the device expects before it will react
// to anything else ("wake the screen", followed by an initial brightness
// command).
func (a *ajazzDevice) initialize() error {
	if err := a.sendSimple("DIS"); err != nil {
		return err
	}
	return a.sendCommand("LIG", 0x00, 0x00, 0x00, 0x00)
}

// Close releases the connection with the device.
func (a *ajazzDevice) Close() error {
	a.cancelSleepTimer()

	a.mu.Lock()
	dev := a.dev
	a.dev = nil
	a.mu.Unlock()

	if dev == nil {
		return nil
	}
	return dev.Close()
}

// FirmwareVersion returns the firmware version of the device.
func (a *ajazzDevice) FirmwareVersion() (string, error) {
	a.mu.Lock()
	dev := a.dev
	a.mu.Unlock()
	if dev == nil {
		return "", fmt.Errorf("ajazz: device not open")
	}

	buf := make([]byte, 20)
	buf[0] = 0x01
	n, err := dev.GetFeatureReport(buf)
	if err != nil {
		return "", err
	}
	if n < 1 {
		return "", nil
	}

	return strings.TrimRight(string(buf[1:n]), "\x00"), nil
}

// Reset re-initializes the device, clears every button image and restores
// the last known brightness.
func (a *ajazzDevice) Reset() error {
	if err := a.clearAll(); err != nil {
		return err
	}
	return a.setBrightness(a.brightness)
}

// Clear sets a blank image on every button.
func (a *ajazzDevice) Clear() error {
	return a.clearAll()
}

func (a *ajazzDevice) clearAll() error {
	if err := a.sendCommand("CLE", 0x00, 0x00, 0x00, 0xFF); err != nil {
		return err
	}
	return a.sendSimple("STP")
}

// Asleep returns true if the device is asleep.
func (a *ajazzDevice) Asleep() bool {
	a.sleepMutex.RLock()
	defer a.sleepMutex.RUnlock()
	return a.asleep
}

// Sleep fades the brightness down to 0 and puts the device into its low
// power state.
func (a *ajazzDevice) Sleep() error {
	a.sleepMutex.Lock()
	defer a.sleepMutex.Unlock()

	a.preSleepBrightness = a.brightness
	if err := a.fade(a.brightness, 0); err != nil {
		return err
	}

	a.asleep = true
	if err := a.setBrightness(0); err != nil {
		return err
	}
	return a.sendSimple("HAN")
}

// Wake wakes the device from sleep and fades the brightness back in.
func (a *ajazzDevice) Wake() error {
	a.sleepMutex.Lock()
	defer a.sleepMutex.Unlock()

	a.asleep = false
	if err := a.sendSimple("DIS"); err != nil {
		return err
	}
	if err := a.fade(0, a.preSleepBrightness); err != nil {
		return err
	}

	a.lastAction = time.Now()
	return a.setBrightness(a.preSleepBrightness)
}

func (a *ajazzDevice) cancelSleepTimer() {
	if a.sleepCancel == nil {
		return
	}
	a.sleepCancel()
	a.sleepCancel = nil
}

// SetSleepFadeDuration sets the duration of the fading animation when the
// device is put to sleep or wakes up.
func (a *ajazzDevice) SetSleepFadeDuration(t time.Duration) {
	a.fadeDuration = t
}

// SetSleepTimeout sets the time after which the device will sleep if no key
// events are received.
func (a *ajazzDevice) SetSleepTimeout(t time.Duration) {
	a.cancelSleepTimer()
	if t == 0 {
		return
	}

	var ctx context.Context
	ctx, a.sleepCancel = context.WithCancel(context.Background())

	go func() {
		for {
			select {
			case <-time.After(time.Second):
				a.sleepMutex.RLock()
				since := time.Since(a.lastAction)
				asleep := a.asleep
				a.sleepMutex.RUnlock()

				if !asleep && since >= t {
					_ = a.Sleep()
				}

			case <-ctx.Done():
				return
			}
		}
	}()
}

// fade smoothly steps the brightness from start to end over fadeDuration.
func (a *ajazzDevice) fade(start, end uint8) error {
	step := (float64(end) - float64(start)) / float64(a.fadeDuration/ajazzFadeDelay)
	if math.IsInf(step, 0) {
		return nil
	}

	for current := float64(start); ; current += step {
		if !((start < end && uint8(current) < end) ||
			(start > end && uint8(current) > end)) {
			break
		}
		if err := a.setBrightness(uint8(current)); err != nil {
			return err
		}
		time.Sleep(ajazzFadeDelay)
	}
	return nil
}

// SetBrightness sets the background lighting brightness from 0 to 100 percent.
func (a *ajazzDevice) SetBrightness(percent uint8) error {
	if percent > 100 {
		percent = 100
	}
	a.brightness = percent

	if a.Asleep() && percent > 0 {
		// remember the brightness for when the device wakes up, but don't
		// actually change it while asleep
		a.sleepMutex.Lock()
		a.preSleepBrightness = percent
		a.sleepMutex.Unlock()
		return nil
	}

	return a.setBrightness(percent)
}

// setBrightness sends the raw device command, bypassing sleep bookkeeping.
func (a *ajazzDevice) setBrightness(percent uint8) error {
	return a.sendCommand("LIG", 0x00, 0x00, percent)
}

// SetImage sets the image of a button on the device. Keys beyond the 9
// physical buttons (i.e. the 3 rotary encoder presses) have no screen, so
// setting their image is a no-op.
func (a *ajazzDevice) SetImage(index uint8, img image.Image) error {
	if index >= a.profile.rows*a.profile.columns {
		return nil
	}

	size := a.profile.imageSize
	if img.Bounds().Dx() != size || img.Bounds().Dy() != size {
		return fmt.Errorf("supplied image has wrong dimensions, expected %[1]dx%[1]d pixels", size)
	}

	data, err := a.encodeImage(img)
	if err != nil {
		return fmt.Errorf("cannot convert image data: %w", err)
	}

	if len(data) > 65535 {
		return fmt.Errorf("image data too large for device")
	}
	dataLen := len(data) // bounds-checked above to be <= 65535
	if err := a.sendCommand("BAT",
		0x00, 0x00, byte(dataLen>>8), byte(dataLen), index+1); err != nil { //nolint:gosec // dataLen bounds-checked above
		return err
	}
	if err := a.writeImageData(data); err != nil {
		return err
	}
	return a.sendSimple("STP")
}

// encodeImage converts img to the JPEG payload the device expects.
func (a *ajazzDevice) encodeImage(img image.Image) ([]byte, error) {
	rgba := toRGBA(img)
	if a.profile.rotate {
		rgba = rotateClockwise(rgba)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// sendCommand builds and sends a "CRT" prefixed command.
func (a *ajazzDevice) sendCommand(cmd string, params ...byte) error {
	payload := make([]byte, 0, 5+len(cmd)+len(params))
	payload = append(payload, 0x43, 0x52, 0x54, 0x00, 0x00) // "CRT" + 2 zero bytes
	payload = append(payload, cmd...)
	payload = append(payload, params...)
	return a.writeExtended(payload)
}

func (a *ajazzDevice) sendSimple(cmd string) error {
	return a.sendCommand(cmd)
}

// writeExtended sends a single HID output report containing payload,
// zero-padded to the device's packet size.
func (a *ajazzDevice) writeExtended(payload []byte) error {
	if len(payload) > a.profile.packetSize {
		return fmt.Errorf("ajazz: command payload too large (%d > %d bytes)", len(payload), a.profile.packetSize)
	}

	buf := make([]byte, 1+a.profile.packetSize)
	copy(buf[1:], payload)
	return a.write(buf)
}

// writeImageData splits raw image bytes into packetSize-sized HID output
// reports and sends them one after another.
func (a *ajazzDevice) writeImageData(data []byte) error {
	for offset := 0; offset < len(data); offset += a.profile.packetSize {
		end := offset + a.profile.packetSize
		if end > len(data) {
			end = len(data)
		}

		buf := make([]byte, 1+a.profile.packetSize)
		copy(buf[1:], data[offset:end])
		if err := a.write(buf); err != nil {
			return err
		}
	}
	return nil
}

func (a *ajazzDevice) write(buf []byte) error {
	a.mu.Lock()
	dev := a.dev
	a.mu.Unlock()
	if dev == nil {
		return fmt.Errorf("ajazz: device not open")
	}

	_, err := dev.Write(buf)
	return err
}

// ReadKeys returns a channel emitting key press/release events, translating
// the device's raw input codes (buttons, blank buttons and encoder presses)
// into Key events. Encoder rotation isn't mapped to a Key event, since
// deckedmaster has no concept of a continuous/relative input; it's logged at
// verbose level instead.
func (a *ajazzDevice) ReadKeys() (chan Key, error) {
	a.mu.Lock()
	dev := a.dev
	a.mu.Unlock()
	if dev == nil {
		return nil, fmt.Errorf("ajazz: device not open")
	}

	kch := make(chan Key)
	go func() {
		defer close(kch)

		buf := make([]byte, ajazzReadSize)
		var buttons [9]bool
		var encoders [3]bool

		emit := func(idx uint8, pressed bool, state *bool) {
			if !a.profile.bothKeyStates {
				if pressed {
					kch <- Key{Index: idx, Pressed: true}
					kch <- Key{Index: idx, Pressed: false}
				}
				return
			}
			if *state != pressed {
				*state = pressed
				kch <- Key{Index: idx, Pressed: pressed}
			}
		}

		for {
			a.mu.Lock()
			dev := a.dev
			a.mu.Unlock()
			if dev == nil {
				return
			}

			n, err := dev.Read(buf)
			if err != nil {
				return
			}
			if n < 11 || buf[0] != 'A' || buf[1] != 'C' || buf[2] != 'K' {
				continue
			}

			if a.Asleep() {
				// any input wakes the device, but doesn't trigger an action
				_ = a.Wake()
				continue
			}

			a.sleepMutex.Lock()
			a.lastAction = time.Now()
			a.sleepMutex.Unlock()

			input := buf[9]
			state := buf[10]
			pressed := state != 0

			switch {
			case input == 0:
				// idle/heartbeat report: release anything still marked down
				for i := range buttons {
					if buttons[i] {
						buttons[i] = false
						kch <- Key{Index: uint8(i), Pressed: false}
					}
				}
				for i := range encoders {
					if encoders[i] {
						encoders[i] = false
						kch <- Key{Index: 9 + uint8(i), Pressed: false}
					}
				}

			case input >= 1 && input <= 6:
				emit(input-1, pressed, &buttons[input-1])

			case input == 0x25:
				emit(6, pressed, &buttons[6])
			case input == 0x30:
				emit(7, pressed, &buttons[7])
			case input == 0x31:
				emit(8, pressed, &buttons[8])

			case input == 0x33: // left encoder press
				emit(9, pressed, &encoders[0])
			case input == 0x35: // middle/top encoder press
				emit(10, pressed, &encoders[1])
			case input == 0x34: // right encoder press
				emit(11, pressed, &encoders[2])

			// Encoder twist events are relative (one report per detent) and
			// have no press/release state, so they're reported as Key events
			// with Rotation set instead of Pressed, using the same key index
			// as that encoder's press (9=left, 10=middle/top, 11=right).
			case input == 0x90: // left encoder, turned counter-clockwise
				kch <- Key{Index: 9, Rotation: -1}
			case input == 0x91: // left encoder, turned clockwise
				kch <- Key{Index: 9, Rotation: 1}
			case input == 0x50: // middle/top encoder, turned counter-clockwise
				kch <- Key{Index: 10, Rotation: -1}
			case input == 0x51: // middle/top encoder, turned clockwise
				kch <- Key{Index: 10, Rotation: 1}
			case input == 0x60: // right encoder, turned counter-clockwise
				kch <- Key{Index: 11, Rotation: -1}
			case input == 0x61: // right encoder, turned clockwise
				kch <- Key{Index: 11, Rotation: 1}
			}
		}
	}()

	return kch, nil
}

// toRGBA converts an image.Image to an image.RGBA.
func toRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out
}

// rotateClockwise returns img rotated 90 degrees clockwise.
func rotateClockwise(img *image.RGBA) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	out := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(h-1-y, x, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}
