package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/fsnotify/fsnotify"
)

var (
	SPACES = regexp.MustCompile(`\s+`)
	PATH   = strings.Split(os.Getenv("PATH"), ":")
)

type WindowWidgets struct {
	resource regexp.Regexp
	title    regexp.Regexp
	widgets  map[uint8]Widget
}

// Deck is a set of widgets.
type Deck struct {
	file       string
	dev        *Device
	config     DeckConfig
	background image.Image
	windows    []WindowWidgets
	overrides  map[uint8]*Widget
	widgets    map[uint8]Widget
}

// LoadDeck loads a deck configuration.
func LoadDeck(dev *Device, base string, deck string) (*Deck, error) {
	path, err := expandPath(base, deck)
	if err != nil {
		return nil, err
	}
	currentDeck = path
	verboseLog("Loading deck: %s", path)

	dc, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}

	d := Deck{
		overrides: make(map[uint8]*Widget),
		widgets:   make(map[uint8]Widget),
		file:      path,
		dev:       dev,
		config:    dc,
	}
	if err := d.applyConfig(dc); err != nil {
		return nil, err
	}

	if err := LoadWidgets(&d); err != nil {
		return nil, err
	}

	for _, w := range dc.Windows {
		if e := d.addWindow(dev, &w); e != nil {
			return nil, e
		}
	}

	if e := d.watchForChanges(dev, base, deck, path); e != nil {
		// a missing file watch is not fatal, deckedmaster just won't auto-reload
		errorLog(e, "can't watch deck for changes")
	}

	return &d, nil
}

// applyConfig applies deck-level configuration (currently the background
// image). Split out so the REST API can re-apply a config to a live deck.
func (d *Deck) applyConfig(dc DeckConfig) error {
	d.config = dc
	if dc.Background == "" {
		return nil
	}

	bgPath, err := expandPath(filepath.Dir(d.file), dc.Background)
	if err != nil {
		return err
	}
	return d.loadBackground(bgPath)
}

// GetDevice returns the streamdeck device this deck is bound to.
func (d *Deck) GetDevice() *Device {
	return d.dev
}

// LoadWidgets (re)builds every widget of a deck from its current config. The
// REST API calls this to apply a configuration change without restarting.
func LoadWidgets(deck *Deck) error {
	keyMap := map[uint8]KeyConfig{}
	names := map[string]bool{}
	for _, k := range deck.config.Keys {
		if k.Name != "" {
			if names[k.Name] {
				return fmt.Errorf("duplicate widgets with the name '%s'", k.Name)
			}
			names[k.Name] = true
		}
		keyMap[k.Index] = k
	}

	// close the widgets we are about to replace, so they can release resources
	for i, w := range deck.widgets {
		if e := w.Close(); e != nil {
			errorLog(e, "failed to close widget %d", i)
		}
	}
	deck.widgets = make(map[uint8]Widget)

	for i := uint8(0); i < deck.dev.Keys; i++ {
		w, err := LoadWidget(deck, i, keyMap[i])
		if err != nil {
			return err
		}
		deck.widgets[i] = w
	}
	return nil
}

// LoadWidget builds the widget for a single key.
func LoadWidget(deck *Deck, i uint8, keyConfig KeyConfig) (Widget, error) {
	bg := deck.backgroundForKey(deck.dev, i)
	base := filepath.Dir(deck.file)

	if keyConfig.Index == i && keyConfig.Widget.ID != "" {
		return NewWidget(deck.dev, base, keyConfig, bg)
	}

	// No widget configured for this key (e.g. a rotary encoder position with
	// no screen) - still honor any actions bound to it.
	var action, actionHold, actionRotateLeft, actionRotateRight *ActionConfig
	if keyConfig.Index == i {
		action = keyConfig.Action
		actionHold = keyConfig.ActionHold
		actionRotateLeft = keyConfig.ActionRotateLeft
		actionRotateRight = keyConfig.ActionRotateRight
	}
	return NewBaseWidget(deck.dev, base, i, action, actionHold, actionRotateLeft, actionRotateRight, bg), nil
}

// watchForChanges reloads the deck whenever its config file changes on disk.
// The watcher shuts itself down as soon as another deck has been loaded, so
// only the currently active deck ever triggers a reload.
func (d *Deck) watchForChanges(dev *Device, base, deckName, path string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := watcher.Add(path); err != nil {
		_ = watcher.Close()
		return err
	}

	go func() {
		defer watcher.Close() //nolint:errcheck

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if currentDeck != path {
					// a different deck is active now, stop watching this one
					return
				}
				// editors often replace files rather than writing in place
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
					continue
				}

				verboseLog("Deck %s changed (%s), reloading...", event.Name, event.Op)
				nd, e := LoadDeck(dev, base, deckName)
				if e != nil {
					errorLog(e, "invalid configuration, keeping the current one")
					continue
				}
				if e := dev.Clear(); e != nil {
					fatal(e)
					return
				}

				deck = nd
				deck.updateWidgets()
				return

			case e, ok := <-watcher.Errors:
				if !ok {
					return
				}
				errorLog(e, "error watching deck")
			}
		}
	}()

	return nil
}

func (d *Deck) addWindow(dev *Device, w *WindowConfig) error {
	verboseLog("loading window overrides %s:%s", w.Resource, w.Title)

	resource, err := regexp.Compile(w.Resource)
	if err != nil {
		errorLogF("failed to compile regex: %s", w.Resource)
		return err
	}

	title, err := regexp.Compile(w.Title)
	if err != nil {
		errorLogF("failed to compile regex: %s", w.Title)
		return err
	}

	window := WindowWidgets{
		resource: *resource,
		title:    *title,
		widgets:  make(map[uint8]Widget),
	}
	for _, key := range w.Keys {
		if e := window.addWidget(dev, d, key); e != nil {
			errorLogF("failed to add widget %s:%s[%d]", w.Resource, w.Title, key.Index)
			return e
		}
	}
	d.windows = append(d.windows, window)
	return nil
}

func (ww *WindowWidgets) addWidget(dev *Device, deck *Deck, key KeyConfig) error {
	bg := deck.backgroundForKey(dev, key.Index)
	widget, err := NewWidget(dev, filepath.Dir(deck.file), key, bg)
	if err != nil {
		return err
	}
	ww.widgets[key.Index] = widget
	return nil
}

func (ww *WindowWidgets) Matches(window ActiveWindow) bool {
	resource := ww.resource.MatchString(window.resource)
	title := ww.title.MatchString(window.title)
	return resource && title
}

// loads a background image.
func (d *Deck) loadBackground(bg string) error {
	f, err := os.Open(bg)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	background, _, err := image.Decode(f)
	if err != nil {
		return err
	}

	if err := d.validateBackground(background); err != nil {
		return err
	}

	d.background = background
	return nil
}

func (d *Deck) validateBackground(background image.Image) error {
	dev := d.dev
	rows := int(dev.Rows)
	cols := int(dev.Columns)
	padding := int(dev.Padding)
	pixels := int(dev.Pixels)

	width := cols*pixels + (cols-1)*padding
	height := rows*pixels + (rows-1)*padding
	if background.Bounds().Dx() != width ||
		background.Bounds().Dy() != height {
		return fmt.Errorf("supplied background image has wrong dimensions, expected %dx%d pixels", width, height)
	}

	return nil
}

// returns the background image for an individual key.
func (d *Deck) backgroundForKey(dev *Device, key uint8) image.Image {
	padding := int(dev.Padding)
	pixels := int(dev.Pixels)
	bg := image.NewRGBA(image.Rect(0, 0, pixels, pixels))

	keyConfig := KeyConfig{}
	for _, conf := range d.config.Keys {
		if conf.Index == key {
			keyConfig = conf
			break
		}
	}

	if d.background == nil && keyConfig.BackgroundColor == "" {
		return bg
	}

	start := image.Point{
		X: int(key%dev.Columns) * (pixels + padding),
		Y: int(key/dev.Columns) * (pixels + padding),
	}

	// In "blend" mode the deck background is painted on top of the solid color,
	// so the color only shows through the image's transparent regions.
	blend := keyConfig.BackgroundMode == "blend"

	if d.background != nil && keyConfig.BackgroundColor != "" && !blend {
		draw.Draw(bg, bg.Bounds(), d.background, start, draw.Over)
	}
	if keyConfig.BackgroundColor != "" {
		var fillColor color.Color
		if e := ConfigValue(keyConfig.BackgroundColor, &fillColor); e != nil {
			verboseLog("Unable to parse backgroundColor for key %d", key)
		} else {
			draw.Draw(bg, bg.Bounds(), &image.Uniform{C: fillColor}, start, draw.Src)
		}
	}
	if d.background != nil && (blend || keyConfig.BackgroundColor == "") {
		draw.Draw(bg, bg.Bounds(), d.background, start, draw.Over)
	}

	return bg
}

func (d *Deck) WindowChanged(window ActiveWindow) {
	verboseLog("windowChanged %s:%s %s", window.resource, window.title, window.id)
	var match = false
	for _, w := range d.windows {
		if w.Matches(window) {
			verboseLog("windowMatch: %s:%s", w.resource.String(), w.title.String())
			for i, widget := range w.widgets {
				d.overrideWidget(i, widget)
			}
			match = true
		}
	}
	if !match {
		for key := range d.overrides {
			d.restoreWidget(key)
		}
	}
}

func (d *Deck) overrideWidget(key uint8, widget Widget) {
	d.overrides[key] = &widget
	if err := widget.Update(); err != nil {
		fatal(err)
	}
}

func (d *Deck) restoreWidget(key uint8) {
	delete(d.overrides, key)
	if err := d.widgets[key].Update(); err != nil {
		fatal(err)
	}
}

// ValidateWidgetBackground checks an uploaded image against the deck geometry.
func (d *Deck) ValidateWidgetBackground(background image.Image) error {
	dev := d.dev
	rows := int(dev.Rows)
	cols := int(dev.Columns)
	padding := int(dev.Padding)
	pixels := int(dev.Pixels)

	width := cols*pixels + (cols-1)*padding
	height := rows*pixels + (rows-1)*padding
	if background.Bounds().Dx() != width ||
		background.Bounds().Dy() != height {
		return fmt.Errorf("supplied widget image has wrong dimensions, expected %dx%d pixels", width, height)
	}
	return nil
}

// handles keypress with delay.
func emulateKeyPressWithDelay(keys string) {
	kd := strings.Split(keys, "+")
	emulateKeyPress(kd[0])
	if len(kd) == 1 {
		return
	}

	// optional delay
	if delay, err := strconv.Atoi(strings.TrimSpace(kd[1])); err == nil {
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
}

// emulates a range of key presses.
func emulateKeyPresses(keys string) {
	for _, kp := range strings.Split(keys, "/") {
		emulateKeyPressWithDelay(kp)
	}
}

// emulates a (multi-)key press.
func emulateKeyPress(keys string) {
	if keyboard == nil {
		errorLogF("Keyboard emulation is disabled!")
		return
	}

	kk := strings.Split(keys, "-")
	for i, k := range kk {
		k = formatKeycodes(strings.TrimSpace(k))
		kc, e := strconv.Atoi(k)
		if e != nil {
			errorLog(e, "%s is not a valid keycode", k)
		}

		if i+1 < len(kk) {
			_ = keyboard.KeyDown(kc)
			defer keyboard.KeyUp(kc) //nolint:errcheck
		} else {
			_ = keyboard.KeyPress(kc)
		}
	}
}

// emulates a clipboard paste.
func emulateClipboard(text string) {
	errorLog(clipboard.WriteAll(text), "failed to paste from the Clipboard")

	// paste the string
	emulateKeyPress("29-47") // ctrl-v
}

// executes a dbus method.
func executeDBusMethod(config *DBusConfig) {
	if e := CallDBus(config.Object, config.Path, config.Method, config.Value); e != nil {
		errorLog(e, "DBus call failed %+v", config)
	}
}

func (d *Deck) Widgets(yield func(Widget) bool) {
	for i, w := range d.widgets {
		override := d.overrides[i]
		if override == nil {
			if !yield(w) {
				return
			}
		}
	}
	for i := range d.overrides {
		widget := d.overrides[i]
		if !yield(*widget) {
			return
		}
	}
}

func (d *Deck) widget(key uint8) Widget {
	widget := d.overrides[key]
	if widget != nil {
		return *widget
	}
	return d.widgets[key]
}

// triggerAction triggers an action.
func (d *Deck) triggerAction(dev *Device, index uint8, hold bool) {
	w := d.widget(index)
	w.TriggerAction(hold)

	var a *ActionConfig
	if hold {
		a = w.ActionHold()
	} else {
		a = w.Action()
	}
	d.runAction(dev, a)
}

// triggerRotateAction triggers the action bound to a rotary encoder being
// turned. delta is negative for a counter-clockwise turn, positive for
// clockwise; only its sign is used. Devices without rotary encoders (e.g. a
// genuine Stream Deck) never report rotation, so this is never reached for
// them.
func (d *Deck) triggerRotateAction(dev *Device, index uint8, delta int8) {
	w := d.widget(index)

	var a *ActionConfig
	if delta < 0 {
		a = w.ActionRotateLeft()
	} else {
		a = w.ActionRotateRight()
	}
	d.runAction(dev, a)
}

// runAction executes a single resolved ActionConfig.
func (d *Deck) runAction(dev *Device, a *ActionConfig) {
	if a == nil {
		return
	}
	if a.Deck != "" {
		newDeck, err := LoadDeck(dev, filepath.Dir(d.file), a.Deck)
		if err != nil {
			errorLog(err, "Failed to load deck %s", a.Deck)
			return
		}
		if err := dev.Clear(); err != nil {
			fatal(err)
			return
		}

		// the event loop redraws from the package-level active deck, so it
		// has to be switched too or the previous deck snaps back a moment
		// later. The receiver follows it for the rest of this action.
		deck = newDeck
		d = newDeck
		d.updateWidgets()
	}
	if a.Keycode != "" {
		emulateKeyPresses(a.Keycode)
	}
	if a.Paste != "" {
		emulateClipboard(a.Paste)
	}
	if a.DBus != nil && a.DBus.Method != "" {
		executeDBusMethod(a.DBus)
	}
	if a.Exec != "" {
		errorLog(executeCommand(a.Exec), "failed to execute command")
	}
	if a.Device != "" {
		switch {
		case a.Device == "sleep":
			if err := dev.Sleep(); err != nil {
				fatal(err)
			}

		case strings.HasPrefix(a.Device, "brightness"):
			d.adjustBrightness(dev, strings.TrimPrefix(a.Device, "brightness"))

		case strings.HasPrefix(a.Device, "volume"):
			d.adjustVolume(strings.TrimPrefix(a.Device, "volume"))

		default:
			errorLogF("Unrecognized special action: %s", a.Device)
		}
	}
}

// updateWidgets updates/repaints all the widgets.
func (d *Deck) updateWidgets() {
	updateMutex.Lock()
	for w := range d.Widgets {
		if !w.RequiresUpdate() {
			continue
		}

		// fmt.Println("Repaint", w.Key())
		if err := w.Update(); err != nil {
			fatal(err)
		}
	}
	updateMutex.Unlock()
}

func (d *Deck) replaceBackground(image image.Image) error {
	updateMutex.Lock()
	err := d.validateBackground(image)
	if err != nil {
		updateMutex.Unlock()
		return err
	}
	d.background = image
	for w := range d.Widgets {
		w.reloadBackground()
		// fmt.Println("Repaint", w.Key())
		if err := w.Update(); err != nil {
			fatal(err)
		}
	}
	updateMutex.Unlock()
	return nil
}

// adjustBrightness adjusts the brightness.
func (d *Deck) adjustBrightness(dev *Device, value string) {
	if len(value) == 0 {
		errorLogF("no brightness value specified")
		return
	}

	v := int64(math.MinInt64)
	if len(value) > 1 {
		nv, err := strconv.ParseInt(value[1:], 10, 64)
		if err == nil {
			v = nv
		}
	}

	switch value[0] {
	case '=': // brightness=[n]:
	case '-': // brightness-[n]:
		if v == math.MinInt64 {
			v = 10
		}
		v = int64(currentBrightness()) - v
	case '+': // brightness+[n]:
		if v == math.MinInt64 {
			v = 10
		}
		v = int64(currentBrightness()) + v
	default:
		v = math.MinInt64
	}

	if v == math.MinInt64 {
		errorLogF("could not grok the brightness from value '%s'", value)
		return
	}

	if v < 1 {
		v = 1
	}
	brightness := clampBrightness(v)
	if err := dev.SetBrightness(brightness); err != nil {
		fatal(err)
	}

	*brightnessConfig = uint(brightness)
}

// adjustVolume adjusts the default PulseAudio sink's volume. value uses the
// same "=[n]", "-[n]", "+[n]" mini-language as "brightness" actions,
// interpreted as a percentage (0-100).
func (d *Deck) adjustVolume(value string) {
	if len(value) == 0 {
		errorLogF("no volume value specified")
		return
	}

	current, err := pa.Volume()
	if err != nil {
		errorLog(err, "failed to read current volume")
		return
	}

	v := int64(math.MinInt64)
	if len(value) > 1 {
		nv, err := strconv.ParseInt(value[1:], 10, 64)
		if err == nil {
			v = nv
		}
	}

	switch value[0] {
	case '=': // volume=[n]:
	case '-': // volume-[n]:
		if v == math.MinInt64 {
			v = 5
		}
		v = int64(math.Round(float64(current)*100)) - v
	case '+': // volume+[n]:
		if v == math.MinInt64 {
			v = 5
		}
		v = int64(math.Round(float64(current)*100)) + v
	default:
		v = math.MinInt64
	}

	if v == math.MinInt64 {
		errorLogF("could not grok the volume from value '%s'", value)
		return
	}

	if v < 0 {
		v = 0
	} else if v > 100 {
		v = 100
	}

	errorLog(pa.SetVolume(float32(v)/100), "failed to set volume")
}
