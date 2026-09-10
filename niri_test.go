//go:build linux

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestNiriCamelCase(t *testing.T) {
	for name, want := range map[string]string{
		"maximize-column":              "MaximizeColumn",
		"toggle-windowed-fullscreen":   "ToggleWindowedFullscreen",
		"focus-window-previous":        "FocusWindowPrevious",
		"close-window":                 "CloseWindow",
		"quit":                         "Quit",
		"MaximizeColumn":               "MaximizeColumn",
		"switch-preset-column-width--": "SwitchPresetColumnWidth",
	} {
		if got := niriCamelCase(name); got != want {
			t.Errorf("niriCamelCase(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestNiriTrunc(t *testing.T) {
	for _, tc := range []struct {
		in    string
		width int
		want  string
	}{
		{"shell", 10, "shell"},
		{"exactly10!", 10, "exactly10!"},
		{"a very long window title", 10, "a very lo."},
		// truncation counts characters, not bytes
		{"日本語のタイトルです", 5, "日本語の."},
		{"anything", 0, "anything"},
	} {
		if got := niriTrunc(tc.in, tc.width); got != tc.want {
			t.Errorf("niriTrunc(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
	}
}

// TestNiriPick covers the direction a dial reports: only the sign matters.
func TestNiriPick(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-1"}, "left"},
		{[]string{"+1"}, "right"},
		{[]string{"1"}, "right"},
		{nil, "right"},
	} {
		if got := niriPick(tc.args, "left", "right"); got != tc.want {
			t.Errorf("niriPick(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestNiriWorkspaceReference(t *testing.T) {
	if got := niriWorkspaceReference("3"); got["Index"] != uint64(3) {
		t.Errorf("niriWorkspaceReference(\"3\") = %v, want an Index", got)
	}
	if got := niriWorkspaceReference("dev"); got["Name"] != "dev" {
		t.Errorf("niriWorkspaceReference(\"dev\") = %v, want a Name", got)
	}
}

// TestNiriOnOutput checks that workspaces are filtered by output and ordered
// by index, which is what cycling past the last one back to the first relies
// on - niri hands them over in an arbitrary order.
func TestNiriOnOutput(t *testing.T) {
	workspaces := []niriWorkspace{
		{Idx: 3, Output: "eDP-1"},
		{Idx: 1, Output: "HDMI-A-1"},
		{Idx: 1, Output: "eDP-1", IsFocused: true},
		{Idx: 2, Output: "eDP-1"},
	}

	on := niriOnOutput(workspaces, "eDP-1")
	if len(on) != 3 {
		t.Fatalf("niriOnOutput() returned %d workspaces, want 3", len(on))
	}
	for i, ws := range on {
		if ws.Idx != int64(i+1) {
			t.Errorf("workspace %d has index %d, want %d", i, ws.Idx, i+1)
		}
	}

	focused := niriFocusedIn(workspaces)
	if focused == nil || focused.Idx != 1 || focused.Output != "eDP-1" {
		t.Errorf("niriFocusedIn() = %+v, want the focused eDP-1 workspace", focused)
	}
}

func TestNiriPayload(t *testing.T) {
	payload, err := niriPayload(json.RawMessage(`{"Workspaces":[{"idx":1}]}`))
	if err != nil {
		t.Fatalf("niriPayload() failed: %v", err)
	}
	if string(payload) != `[{"idx":1}]` {
		t.Errorf("niriPayload() = %s, want the unwrapped list", payload)
	}

	if _, err := niriPayload(json.RawMessage(`"Handled"`)); err == nil {
		t.Error("niriPayload() should reject a reply that is not an object")
	}
}

// TestNiriShowWithoutCompositor covers the headless case: every field has to
// fall back to the placeholder rather than fail, because a widget returning an
// error shuts the daemon down.
func TestNiriShowWithoutCompositor(t *testing.T) {
	t.Setenv(niriSocketEnv, "")

	for field := range niriFields {
		if got := niriShow(field, 10, "-"); got != "-" {
			t.Errorf("niriShow(%q) = %q with no compositor, want the placeholder", field, got)
		}
	}

	// anything that is not a field name is a literal caption
	if got := niriShow("monitor", 10, "-"); got != "monitor" {
		t.Errorf("niriShow(\"monitor\") = %q, want it drawn literally", got)
	}
}

// TestNiriActionPayload pins the wire format of the actions a key binds. The
// request has to come out exactly as niri's Action enum is serialized, and
// that is not something a running compositor tells us gently: it answers an
// action it cannot parse with the same error whatever is wrong with it.
func TestNiriActionPayload(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"maximize-column", nil, `{"Action":{"MaximizeColumn":{}}}`},
		{"toggle-windowed-fullscreen", nil, `{"Action":{"ToggleWindowedFullscreen":{}}}`},
		{"focus-column-left", nil, `{"Action":{"FocusColumnLeft":{}}}`},
		{"focus-workspace", []string{"3"}, `{"Action":{"FocusWorkspace":{"reference":{"Index":3}}}}`},
		{"focus-workspace", []string{"dev"}, `{"Action":{"FocusWorkspace":{"reference":{"Name":"dev"}}}}`},
	} {
		action, err := niriActionPayload(tc.name, tc.args)
		if err != nil {
			t.Errorf("niriActionPayload(%q, %v) failed: %v", tc.name, tc.args, err)
			continue
		}
		got, err := json.Marshal(niriActionRequest(action))
		if err != nil {
			t.Errorf("marshalling %q: %v", tc.name, err)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("niriActionPayload(%q, %v) sends %s, want %s", tc.name, tc.args, got, tc.want)
		}
	}

	// an action with arguments we don't understand has to say so rather than
	// silently drop them and perform something else
	if _, err := niriActionPayload("set-column-width", []string{"50%"}); err == nil {
		t.Error("niriActionPayload() should reject arguments it can't place")
	}
}

// TestNiriWidgetFromBundledDeck builds the widgets of the shipped niri deck
// from its actual TOML, which is the seam between what a deck file says and
// what a key ends up drawing.
func TestNiriWidgetFromBundledDeck(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join("decks", "niri.deck"))
	if err != nil {
		t.Fatalf("LoadConfig(decks/niri.deck): %v", err)
	}

	dev := &Device{Columns: 3, Rows: 3, Keys: 12, Pixels: 72, DPI: 72}
	want := map[uint8][]string{
		0: {"ws-name", "ws-idx", "ws-count"},
		1: {"win-app", "win-title", "win-pos"},
		2: {"Overview", "overview"},
		5: {"Monitor", "output", "output-count"},
	}

	built := 0
	for _, key := range cfg.Keys {
		if key.Widget.ID != "niri" {
			continue
		}
		built++

		widget, err := NewNiriWidget(NewBaseWidget(dev, "decks", key.Index, nil, nil, nil, nil, nil), key.Widget)
		if err != nil {
			t.Errorf("key %d: %v", key.Index, err)
			continue
		}
		w, ok := widget.(*NiriWidget)
		if !ok {
			t.Errorf("key %d: NewNiriWidget returned a %T", key.Index, widget)
			continue
		}

		if !slices.Equal(w.fields, want[key.Index]) {
			t.Errorf("key %d draws %v, want %v", key.Index, w.fields, want[key.Index])
		}
		// a font, a color and a frame per field, or drawing one panics
		if len(w.fonts) != len(w.fields) || len(w.colors) != len(w.fields) || len(w.frames) != len(w.fields) {
			t.Errorf("key %d has %d fields but %d fonts, %d colors and %d frames",
				key.Index, len(w.fields), len(w.fonts), len(w.colors), len(w.frames))
		}
		if w.width != niriDefaultWidth || w.placeholder != niriDefaultPlaceholder {
			t.Errorf("key %d has width %d and placeholder %q, want the defaults",
				key.Index, w.width, w.placeholder)
		}
	}

	if built != len(want) {
		t.Errorf("built %d niri widgets from decks/niri.deck, want %d", built, len(want))
	}
}

// TestNiriWidgetOverrides checks that a deck can shorten the text and change
// what stands in for state niri does not have.
func TestNiriWidgetOverrides(t *testing.T) {
	dev := &Device{Columns: 3, Rows: 3, Keys: 12, Pixels: 72, DPI: 72}
	opts := WidgetConfig{
		ID: "niri",
		Config: map[string]interface{}{
			"fields":      []interface{}{"win-title"},
			"width":       int64(4),
			"placeholder": "",
		},
	}

	widget, err := NewNiriWidget(NewBaseWidget(dev, "decks", 0, nil, nil, nil, nil, nil), opts)
	if err != nil {
		t.Fatalf("NewNiriWidget() failed: %v", err)
	}
	w := widget.(*NiriWidget)

	if w.width != 4 {
		t.Errorf("width = %d, want 4", w.width)
	}
	if w.placeholder != "" {
		t.Errorf("placeholder = %q, want it empty", w.placeholder)
	}
}

// TestNiriLiveState reads every field from a running compositor. It is skipped
// where there is none, which includes CI. It only reads: nothing here changes
// what the compositor is doing.
func TestNiriLiveState(t *testing.T) {
	if os.Getenv(niriSocketEnv) == "" {
		t.Skipf("$%s is not set, no niri to talk to", niriSocketEnv)
	}

	for field := range niriFields {
		show := niriFields[field]
		value, err := show(10)
		if err != nil {
			t.Errorf("reading '%s' failed: %v", field, err)
			continue
		}
		t.Logf("%-12s %q", field, value)
	}

	outputs, err := niriOutputs()
	if err != nil {
		t.Fatalf("reading outputs failed: %v", err)
	}
	if len(outputs) > 1 {
		t.Skip("more than one output connected, stepping between them is not a no-op")
	}
	// With a single output the monitor dial has to reach the compositor for
	// the count and then do nothing at all, which is the one action path that
	// can be exercised live without moving anything.
	if err := niriCommand("mon-scroll +1"); err != nil {
		t.Errorf("mon-scroll on a single output failed: %v", err)
	}
}
