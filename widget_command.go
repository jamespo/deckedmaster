package main

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"
)

// CommandWidget is a widget displaying the output of command(s).
type CommandWidget struct {
	*BaseWidget

	commands []string
	fonts    []string
	frames   []image.Rectangle
	colors   []color.Color
}

// NewCommandWidget returns a new CommandWidget.
func NewCommandWidget(bw *BaseWidget, opts WidgetConfig) *CommandWidget {
	bw.setInterval(opts.IntervalDuration(), time.Second)

	// A setting we can't parse leaves its destination empty, which paints a
	// blank key - silently, unless we say something here.
	config := func(key string, dst interface{}) {
		v, ok := opts.Config[key]
		if !ok {
			return
		}
		errorLog(ConfigValue(v, dst), "command widget on key %d: bad '%s' setting", bw.key, key)
	}

	var commands, fonts, frameReps []string
	config("command", &commands)
	config("font", &fonts)
	config("layout", &frameReps)
	var colors []color.Color
	config("color", &colors)

	layout := NewLayout(int(bw.dev.Pixels))
	frames := layout.FormatLayout(frameReps, len(commands))

	for i := 0; i < len(commands); i++ {
		if len(fonts) < i+1 {
			fonts = append(fonts, "regular")
		}
		if len(colors) < i+1 {
			colors = append(colors, DefaultColor)
		}
	}

	return &CommandWidget{
		BaseWidget: bw,
		commands:   commands,
		fonts:      fonts,
		frames:     frames,
		colors:     colors,
	}
}

// Update renders the widget.
func (w *CommandWidget) Update() error {
	size := int(w.dev.Pixels)
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	for i := 0; i < len(w.commands); i++ {
		str, err := runCommand(w.commands[i])
		if err != nil {
			return fmt.Errorf("key %d: command '%s': %w", w.key, w.commands[i], err)
		}
		verboseLog("key %d: command '%s' printed '%s'", w.key, w.commands[i], str)
		font := fontByName(w.fonts[i])

		drawString(img,
			w.frames[i],
			font,
			str,
			w.dev.DPI,
			-1,
			w.colors[i],
			image.Pt(-1, -1))
	}
	return w.render(w.dev, img)
}

func runCommand(command string) (string, error) {
	output, err := shellCommand(command).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(output), "\n"), nil
}
