//go:build linux

package main

import (
	"image"
	"image/color"
	"slices"
	"time"
)

const (
	// niriDefaultWidth is how many characters of a window title or workspace
	// name fit on a key before it turns into a smudge.
	niriDefaultWidth = 10
	// niriDefaultPlaceholder is drawn in place of state niri does not have.
	niriDefaultPlaceholder = "-"
)

// NiriWidget draws the state of the niri Wayland compositor: one field per
// line, laid out the way the command widget lays out command output.
type NiriWidget struct {
	*BaseWidget

	fields      []string
	fonts       []string
	frames      []image.Rectangle
	colors      []color.Color
	width       int
	placeholder string

	// the last values painted, so an unchanged key is not redrawn
	drawn []string
}

// NewNiriWidget returns a new NiriWidget.
func NewNiriWidget(bw *BaseWidget, opts WidgetConfig) (Widget, error) {
	bw.setInterval(opts.IntervalDuration(), time.Second)

	// A setting we can't parse leaves its destination empty, which paints a
	// blank key - silently, unless we say something here.
	config := func(key string, dst interface{}) {
		v, ok := opts.Config[key]
		if !ok {
			return
		}
		errorLog(ConfigValue(v, dst), "niri widget on key %d: bad '%s' setting", bw.key, key)
	}

	var fields, fonts, frameReps []string
	config("fields", &fields)
	config("font", &fonts)
	config("layout", &frameReps)
	var colors []color.Color
	config("color", &colors)
	var placeholder string
	config("placeholder", &placeholder)
	width := int64(niriDefaultWidth)
	config("width", &width)

	layout := NewLayout(int(bw.dev.Pixels))
	frames := layout.FormatLayout(frameReps, len(fields))

	for i := 0; i < len(fields); i++ {
		if len(fonts) < i+1 {
			fonts = append(fonts, "regular")
		}
		if len(colors) < i+1 {
			colors = append(colors, DefaultColor)
		}
	}

	if _, ok := opts.Config["placeholder"]; !ok {
		placeholder = niriDefaultPlaceholder
	}

	return &NiriWidget{
		BaseWidget:  bw,
		fields:      fields,
		fonts:       fonts,
		frames:      frames,
		colors:      colors,
		width:       int(width),
		placeholder: placeholder,
	}, nil
}

// Update renders the widget.
func (w *NiriWidget) Update() error {
	values := make([]string, len(w.fields))
	for i, field := range w.fields {
		values[i] = niriShow(field, w.width, w.placeholder)
	}

	// Most refreshes find the compositor exactly where they left it. Marking
	// the key painted without repainting it keeps it off the USB bus and out
	// of the next interval's way.
	if slices.Equal(values, w.drawn) {
		w.lastUpdate = time.Now()
		return nil
	}

	size := int(w.dev.Pixels)
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	for i, value := range values {
		verboseLog("key %d: niri field '%s' is '%s'", w.key, w.fields[i], value)
		if value == "" {
			continue
		}

		drawString(img,
			w.frames[i],
			fontByName(w.fonts[i]),
			value,
			w.dev.DPI,
			-1,
			w.colors[i],
			image.Pt(-1, -1))
	}

	w.drawn = values
	return w.render(w.dev, img)
}

// reloadBackground drops the painted values along with the background, so the
// next update repaints the key rather than deciding nothing has changed.
func (w *NiriWidget) reloadBackground() {
	w.BaseWidget.reloadBackground()
	w.drawn = nil
}
