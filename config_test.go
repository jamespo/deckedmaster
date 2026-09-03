package main

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// widgetIDs lists every widget id NewWidget knows about. It is the contract the
// bundled decks and the README are checked against.
var widgetIDs = []string{
	"audio",
	"button",
	"clock",
	"command",
	"date",
	"imageCommand",
	"mediaPlayer",
	"microphoneMute",
	"mute",
	"pulseAudioControl",
	"recentWindow",
	"spotify",
	"temp",
	"time",
	"timer",
	"top",
	"weather",
}

// Every .deck file shipped in decks/ must parse and only reference widget ids
// that NewWidget actually handles.
func TestBundledDecksAreValid(t *testing.T) {
	known := make(map[string]bool, len(widgetIDs))
	for _, id := range widgetIDs {
		known[id] = true
	}

	matches, err := filepath.Glob(filepath.Join("decks", "*.deck"))
	if err != nil {
		t.Fatalf("globbing decks: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no .deck files found in decks/")
	}

	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			cfg, err := LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig(%s): %v", path, err)
			}
			if len(cfg.Keys) == 0 {
				t.Errorf("%s declares no keys", path)
			}

			seen := map[uint8]bool{}
			for _, k := range cfg.Keys {
				if seen[k.Index] {
					t.Errorf("%s: duplicate key index %d", path, k.Index)
				}
				seen[k.Index] = true

				if k.Widget.ID == "" {
					continue
				}
				if !known[k.Widget.ID] {
					t.Errorf("%s: key %d uses unknown widget id %q", path, k.Index, k.Widget.ID)
				}
			}
		})
	}
}

// Deck inheritance via `parent` must merge keys and detect cycles.
func TestDeckInheritance(t *testing.T) {
	dir := t.TempDir()

	parent := filepath.Join(dir, "parent.deck")
	child := filepath.Join(dir, "child.deck")

	writeFile(t, parent, `
background = "bg.png"

[[keys]]
  index = 0
  [keys.widget]
    id = "time"

[[keys]]
  index = 1
  [keys.widget]
    id = "top"
`)
	writeFile(t, child, `
parent = "parent.deck"

[[keys]]
  index = 1
  [keys.widget]
    id = "weather"
`)

	cfg, err := LoadConfig(child)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Background != "bg.png" {
		t.Errorf("background not inherited from parent, got %q", cfg.Background)
	}
	if len(cfg.Keys) != 2 {
		t.Fatalf("expected 2 merged keys, got %d", len(cfg.Keys))
	}

	byIndex := map[uint8]string{}
	for _, k := range cfg.Keys {
		byIndex[k.Index] = k.Widget.ID
	}
	if byIndex[0] != "time" {
		t.Errorf("key 0 should be inherited as %q, got %q", "time", byIndex[0])
	}
	if byIndex[1] != "weather" {
		t.Errorf("key 1 should be overridden to %q, got %q", "weather", byIndex[1])
	}
}

func TestCircularParentIsRejected(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.deck")
	b := filepath.Join(dir, "b.deck")

	writeFile(t, a, "parent = \"b.deck\"\n[[keys]]\n  index = 0\n")
	writeFile(t, b, "parent = \"a.deck\"\n[[keys]]\n  index = 1\n")

	if _, err := LoadConfig(a); err == nil {
		t.Fatal("expected circular reference to be rejected")
	}
}

// ConfigValue is the conversion layer every widget reads its options through.
// The duration cases arrived with the timer widget (PR #66).
func TestConfigValueConversions(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		var got string
		if err := ConfigValue("hello", &got); err != nil {
			t.Fatal(err)
		}
		if got != "hello" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("bool from string", func(t *testing.T) {
		var got bool
		if err := ConfigValue("true", &got); err != nil {
			t.Fatal(err)
		}
		if !got {
			t.Error("expected true")
		}
	})

	t.Run("int64 from float64", func(t *testing.T) {
		var got int64
		if err := ConfigValue(float64(42), &got); err != nil {
			t.Fatal(err)
		}
		if got != 42 {
			t.Errorf("got %d", got)
		}
	})

	t.Run("color from hex", func(t *testing.T) {
		var got color.Color
		if err := ConfigValue("#ff0000", &got); err != nil {
			t.Fatal(err)
		}
		if got == nil {
			t.Fatal("got nil color")
		}
		r, g, b, _ := got.RGBA()
		if r == 0 || g != 0 || b != 0 {
			t.Errorf("expected pure red, got r=%d g=%d b=%d", r, g, b)
		}
	})

	t.Run("string slice", func(t *testing.T) {
		var got []string
		if err := ConfigValue("a;b;c", &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 || got[0] != "a" || got[2] != "c" {
			t.Errorf("got %#v", got)
		}
	})

	t.Run("duration", func(t *testing.T) {
		var got time.Duration
		if err := ConfigValue("1h5m", &got); err != nil {
			t.Fatal(err)
		}
		if got != 65*time.Minute {
			t.Errorf("got %s", got)
		}
	})

	t.Run("duration slice", func(t *testing.T) {
		var got []time.Duration
		if err := ConfigValue("5s;10m;30m", &got); err != nil {
			t.Fatal(err)
		}
		want := []time.Duration{5 * time.Second, 10 * time.Minute, 30 * time.Minute}
		if len(got) != len(want) {
			t.Fatalf("got %d durations, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("index %d: got %s, want %s", i, got[i], want[i])
			}
		}
	})

	t.Run("type mismatch is an error", func(t *testing.T) {
		var got string
		if err := ConfigValue(123, &got); err == nil {
			t.Error("expected an error converting int to string")
		}
	})
}

// PR #134 added per-key background colors, PR #133 added key names and JSON
// tags, PR #147 added per-window overrides. They must all survive a round trip.
func TestKeyAndWindowConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.deck")

	writeFile(t, path, `
[[keys]]
  index = 3
  name = "record"
  backgroundColor = "#ff0000"
  backgroundMode = "blend"
  [keys.widget]
    id = "button"
  [keys.action]
    keycode = "1"
    [keys.action.dbus]
      object = "org.example"
      method = "org.example.Go"

[[window]]
  resource = "firefox"
  title = ".*YouTube.*"
  [[window.keys]]
    index = 0
    [window.keys.widget]
      id = "mediaPlayer"
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if len(cfg.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(cfg.Keys))
	}
	k := cfg.Keys[0]
	if k.Name != "record" {
		t.Errorf("name: got %q", k.Name)
	}
	if k.BackgroundColor != "#ff0000" {
		t.Errorf("backgroundColor: got %q", k.BackgroundColor)
	}
	if k.BackgroundMode != "blend" {
		t.Errorf("backgroundMode: got %q", k.BackgroundMode)
	}
	if k.Action == nil {
		t.Fatal("action not parsed")
	}
	if k.Action.DBus == nil || k.Action.DBus.Method != "org.example.Go" {
		t.Errorf("dbus action not parsed: %#v", k.Action.DBus)
	}

	if len(cfg.Windows) != 1 {
		t.Fatalf("expected 1 window override, got %d", len(cfg.Windows))
	}
	w := cfg.Windows[0]
	if w.Resource != "firefox" {
		t.Errorf("window resource: got %q", w.Resource)
	}
	if len(w.Keys) != 1 || w.Keys[0].Widget.ID != "mediaPlayer" {
		t.Errorf("window keys not parsed: %#v", w.Keys)
	}
}

// Rotary encoders (e.g. on the Ajazz AKP03E) bind actions separately from a
// regular keypress, via action_rotate_left/action_rotate_right.
func TestRotateActionConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rotate.deck")

	writeFile(t, path, `
[[keys]]
  index = 9
  [keys.action]
    keycode = "Mute"
  [keys.action_rotate_left]
    device = "volume-5"
  [keys.action_rotate_right]
    device = "volume+5"
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if len(cfg.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(cfg.Keys))
	}
	k := cfg.Keys[0]

	if k.ActionRotateLeft == nil || k.ActionRotateLeft.Device != "volume-5" {
		t.Errorf("action_rotate_left not parsed: %#v", k.ActionRotateLeft)
	}
	if k.ActionRotateRight == nil || k.ActionRotateRight.Device != "volume+5" {
		t.Errorf("action_rotate_right not parsed: %#v", k.ActionRotateRight)
	}
	if k.Action == nil || k.Action.Keycode != "Mute" {
		t.Errorf("action not parsed: %#v", k.Action)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
