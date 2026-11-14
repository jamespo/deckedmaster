package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseModifiers(t *testing.T) {
	for in, want := range map[string]Modifiers{
		"super":           ModSuper,
		"Ctrl":            ModCtrl,
		"ctrl+shift":      ModCtrl | ModShift,
		"Shift-Control":   ModCtrl | ModShift,
		"alt + win":       ModAlt | ModSuper,
		"meta+logo+super": ModSuper,
	} {
		got, err := ParseModifiers(in)
		if err != nil {
			t.Errorf("ParseModifiers(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseModifiers(%q) = %s, want %s", in, got, want)
		}
	}

	for _, in := range []string{"", "+", "hyper", "ctrl+caps"} {
		if _, err := ParseModifiers(in); err == nil {
			t.Errorf("ParseModifiers(%q): expected an error", in)
		}
	}
}

func TestActionForModifiers(t *testing.T) {
	a := &ActionConfig{
		Exec: "base",
		Modifiers: map[string]*ActionConfig{
			"super":      {Exec: "super"},
			"ctrl+shift": {Exec: "ctrl+shift"},
		},
	}

	for held, want := range map[Modifiers]string{
		0:                   "base",
		ModSuper:            "super",
		ModShift | ModCtrl:  "ctrl+shift",
		ModCtrl:             "base", // no exact match falls back
		ModSuper | ModShift: "base",
	} {
		got, modified := a.forModifiers(held)
		if got.Exec != want {
			t.Errorf("forModifiers(%s) = %q, want %q", held, got.Exec, want)
		}
		if modified != (want != "base") {
			t.Errorf("forModifiers(%s) modified = %v", held, modified)
		}
	}

	var nilAction *ActionConfig
	if got, modified := nilAction.forModifiers(ModCtrl); got != nil || modified {
		t.Errorf("nil action resolved to %#v, %v", got, modified)
	}
}

func TestModifierActionConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "modifiers.deck")

	writeFile(t, path, `
[[keys]]
  index = 0
  [keys.action]
    exec = "firefox"
    [keys.action.modifiers.super]
      exec = "firefox --private-window"
    [keys.action.modifiers."ctrl+shift"]
      keycode = "Leftctrl-T"
  [keys.action_rotate_left]
    device = "volume-5"
    [keys.action_rotate_left.modifiers.shift]
      device = "volume-1"
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	k := cfg.Keys[0]
	if a := k.Action.Modifiers["super"]; a == nil || a.Exec != "firefox --private-window" {
		t.Errorf("super modifier not parsed: %#v", a)
	}
	if a := k.Action.Modifiers["ctrl+shift"]; a == nil || a.Keycode != "Leftctrl-T" {
		t.Errorf("ctrl+shift modifier not parsed: %#v", a)
	}
	if a, _ := k.ActionRotateLeft.forModifiers(ModShift); a.Device != "volume-1" {
		t.Errorf("rotate shift modifier not resolved: %#v", a)
	}
}

func TestModifierActionConfigValidation(t *testing.T) {
	for name, tc := range map[string]struct{ deck, err string }{
		"unknown modifier": {`
[[keys]]
  index = 0
  [keys.action.modifiers.hyper]
    exec = "x"
`, "unknown modifier"},
		"duplicate combination": {`
[[keys]]
  index = 0
  [keys.action.modifiers."ctrl+shift"]
    exec = "x"
  [keys.action.modifiers."shift+ctrl"]
    exec = "y"
`, "are the same"},
		"nested modifiers": {`
[[keys]]
  index = 0
  [keys.action.modifiers.ctrl]
    exec = "x"
    [keys.action.modifiers.ctrl.modifiers.shift]
      exec = "y"
`, "modifiers of its own"},
		"window override": {`
[[window]]
  resource = "firefox"
  [[window.keys]]
    index = 0
    [window.keys.action_hold.modifiers.bogus]
      exec = "x"
`, "unknown modifier"},
	} {
		path := filepath.Join(t.TempDir(), "bad.deck")
		writeFile(t, path, tc.deck)
		_, err := LoadConfig(path)
		if err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Errorf("%s: expected error containing %q, got %v", name, tc.err, err)
		}
	}
}

// The shipped example deck must stay loadable.
func TestModifiersExampleDeck(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join("decks", "modifiers.deck"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Keys) == 0 {
		t.Fatal("example deck has no keys")
	}
}
