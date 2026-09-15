//go:build linux

package main

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// umbrielFixture is one snapshot of each event, in the shape `umbriel
// subscribe` actually streams them: a single line per event, whose data is the
// full state of that family.
//
// The windows here are deliberately awkward. Three of them are marked focused,
// because umbriel marks the window that would take the focus back on every
// occupied workspace, and only one is marked active - the one a key means by
// "the window". Two share an x, because they are stacked in one column.
var umbrielFixture = []string{
	`{"event":"workspaces","data":[
		{"id":"eDP-1:1","index":1,"name":"1","named":false,"layout":"scrolling","output":"eDP-1","active":false,"focused":false,"occupied":true},
		{"id":"eDP-1:2","index":2,"name":"2","named":false,"layout":"dwindle","output":"eDP-1","active":false,"focused":false,"occupied":true},
		{"id":"eDP-1:3","index":3,"name":"3","named":false,"layout":"scrolling","output":"eDP-1","active":true,"focused":true,"occupied":true},
		{"id":"eDP-1:6","index":4,"name":"4","named":false,"layout":"scrolling","output":"eDP-1","active":false,"focused":false,"occupied":false}]}`,
	`{"event":"windows","data":[
		{"id":"a","app_id":"com.mitchellh.ghostty","title":"a terminal with a long title","workspace":"eDP-1:3","active":true,"focused":true,"floating":false,"scratchpad":"","x":10},
		{"id":"b","app_id":"emacs","title":"*scratch*","workspace":"eDP-1:3","active":false,"focused":false,"floating":false,"scratchpad":"","x":1757},
		{"id":"c","app_id":"emacs","title":"config.toml","workspace":"eDP-1:3","active":false,"focused":false,"floating":false,"scratchpad":"","x":1757},
		{"id":"d","app_id":"firefox","title":"a page","workspace":"eDP-1:3","active":false,"focused":false,"floating":false,"scratchpad":"","x":-864},
		{"id":"e","app_id":"org.mozilla.Thunderbird","title":"mail","workspace":"eDP-1:2","active":false,"focused":true,"floating":false,"scratchpad":"","x":10},
		{"id":"f","app_id":"pavucontrol","title":"volume","workspace":"eDP-1:3","active":false,"focused":false,"floating":true,"scratchpad":"","x":500},
		{"id":"g","app_id":"foot","title":"stashed","workspace":"eDP-1:1","active":false,"focused":true,"floating":false,"scratchpad":"term","x":0}]}`,
	`{"event":"overview","data":{"open":false}}`,
	`{"event":"keyboard_layout","data":{"current_index":0,"names":["English (UK)"]}}`,
	`{"event":"submap","data":null}`,
}

// umbrielTestSnapshot returns a snapshot holding the fixture, built the way a
// running session builds one: by applying the lines of the stream.
func umbrielTestSnapshot(t *testing.T) *umbrielSnapshot {
	t.Helper()

	var s umbrielSnapshot
	for i, line := range umbrielFixture {
		if !s.apply([]byte(line)) {
			t.Fatalf("fixture event %d was not applied", i)
		}
	}
	return &s
}

func TestUmbrielTrunc(t *testing.T) {
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
		if got := umbrielTrunc(tc.in, tc.width); got != tc.want {
			t.Errorf("umbrielTrunc(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
	}
}

// TestUmbrielPick covers the direction a dial reports: only the sign matters.
func TestUmbrielPick(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-1"}, "left"},
		{[]string{"+1"}, "right"},
		{[]string{"1"}, "right"},
		{nil, "right"},
	} {
		if got := umbrielPick(tc.args, "left", "right"); got != tc.want {
			t.Errorf("umbrielPick(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

// TestUmbrielSubmapName covers the one event whose shape umbriel's
// documentation leaves open: null is the default context, and the name is read
// whether it arrives bare or wrapped.
func TestUmbrielSubmapName(t *testing.T) {
	for _, tc := range []struct {
		data string
		want string
	}{
		{`null`, ""},
		{``, ""},
		{`"resize"`, "resize"},
		{`{"name":"resize"}`, "resize"},
		{`{}`, ""},
	} {
		got, err := umbrielSubmapName(json.RawMessage(tc.data))
		if err != nil {
			t.Errorf("umbrielSubmapName(%s) failed: %v", tc.data, err)
			continue
		}
		if got != tc.want {
			t.Errorf("umbrielSubmapName(%s) = %q, want %q", tc.data, got, tc.want)
		}
	}

	if _, err := umbrielSubmapName(json.RawMessage(`[1,2]`)); err == nil {
		t.Error("umbrielSubmapName() should reject a shape it can't read a name from")
	}
}

// TestUmbrielApplyIgnoresUnknown checks that a line we can't use is reported as
// not applied, so that it cannot mark a snapshot live on its own.
func TestUmbrielApplyIgnoresUnknown(t *testing.T) {
	var s umbrielSnapshot

	for _, line := range []string{
		`{"event":"theme","data":{"background":"#141419FF"}}`,
		`{"event":"windows","data":"not a list"}`,
		`not json at all`,
	} {
		if s.apply([]byte(line)) {
			t.Errorf("apply(%s) reported an event we can't draw from", line)
		}
	}
	if s.live {
		t.Error("a snapshot that applied nothing should not be live")
	}
}

// TestUmbrielFocusedIn covers the fallback that matters on a multi-output
// session: the focused workspace is the one taking key presses, but a snapshot
// taken as the pointer crosses outputs can have only an active one.
func TestUmbrielFocusedIn(t *testing.T) {
	workspaces := []umbrielWorkspace{
		{ID: "eDP-1:1", Active: true},
		{ID: "HDMI-A-1:1", Focused: true},
	}
	if got := umbrielFocusedIn(workspaces); got == nil || got.ID != "HDMI-A-1:1" {
		t.Errorf("umbrielFocusedIn() = %+v, want the focused workspace", got)
	}

	// nothing focused, so the workspace active on its output stands in
	if got := umbrielFocusedIn(workspaces[:1]); got == nil || got.ID != "eDP-1:1" {
		t.Errorf("umbrielFocusedIn() = %+v, want the active workspace", got)
	}

	if got := umbrielFocusedIn(nil); got != nil {
		t.Errorf("umbrielFocusedIn(nil) = %+v, want nothing", got)
	}
}

// TestUmbrielOnOutput checks that workspaces are filtered by output and ordered
// by index, which is what cycling past the last one back to the first relies
// on.
func TestUmbrielOnOutput(t *testing.T) {
	workspaces := []umbrielWorkspace{
		{Index: 3, Output: "eDP-1"},
		{Index: 1, Output: "HDMI-A-1"},
		{Index: 1, Output: "eDP-1"},
		{Index: 2, Output: "eDP-1"},
	}

	on := umbrielOnOutput(workspaces, "eDP-1")
	if len(on) != 3 {
		t.Fatalf("umbrielOnOutput() returned %d workspaces, want 3", len(on))
	}
	for i, ws := range on {
		if ws.Index != int64(i+1) {
			t.Errorf("workspace %d has index %d, want %d", i, ws.Index, i+1)
		}
	}

	if got := umbrielOutputs(workspaces); !slices.Equal(got, []string{"HDMI-A-1", "eDP-1"}) {
		t.Errorf("umbrielOutputs() = %v, want each output once", got)
	}
}

// TestUmbrielColumnPos checks the position a dial's readout shows: windows
// stacked in one column share an x and count once, floating windows are not in
// the strip at all, and other workspaces don't get counted in.
func TestUmbrielColumnPos(t *testing.T) {
	s := umbrielTestSnapshot(t)
	windows, err := s.readWindows()
	if err != nil {
		t.Fatalf("readWindows() failed: %v", err)
	}
	active, err := s.activeWindow()
	if err != nil {
		t.Fatalf("activeWindow() failed: %v", err)
	}
	if active.ID != "a" {
		t.Fatalf("activeWindow() = %q, want the one marked active", active.ID)
	}

	// on eDP-1:3 the tiled windows sit at x=-864, 10 and 1757 twice, so there
	// are three columns and the active window is the second of them
	pos, count := umbrielColumnPos(windows, active)
	if pos != 2 || count != 3 {
		t.Errorf("umbrielColumnPos() = %d/%d, want 2/3", pos, count)
	}
}

func TestUmbrielWorkspaceReference(t *testing.T) {
	if got := umbrielWorkspaceReference(umbrielWorkspace{Name: "3", Output: "eDP-1"}); got != "3/eDP-1" {
		t.Errorf("umbrielWorkspaceReference() = %q, want \"3/eDP-1\"", got)
	}
	// a name is quoted so that one which looks like a number is not taken for
	// an index
	if got := umbrielWorkspaceReference(umbrielWorkspace{Name: "2", Named: true, Output: "DP-2"}); got != `"2"/DP-2` {
		t.Errorf("umbrielWorkspaceReference() = %q, want a quoted name", got)
	}
}

// TestUmbrielShow renders every field from the fixture, which is the seam
// between what the stream says and what a key ends up drawing.
func TestUmbrielShow(t *testing.T) {
	s := umbrielTestSnapshot(t)

	for field, want := range map[string]string{
		"ws-name":      "3",
		"ws-idx":       "3",
		"ws-count":     "3/4",
		"ws-layout":    "scrolling",
		"win-app":      "ghostty",
		"win-title":    "a termina.",
		"win-pos":      "2/3",
		"win-count":    "5 win",
		"win-float":    "tile",
		"output":       "eDP-1",
		"output-count": "1 mon",
		"overview":     "closed",
		"kb-layout":    "English (.",
		"scratchpad":   "1",
		// no submap is active, so the key falls back to its placeholder
		"submap": "-",
	} {
		if got := s.show(field, 10, "-"); got != want {
			t.Errorf("show(%q) = %q, want %q", field, got, want)
		}
	}

	// anything that is not a field name is a literal caption
	if got := s.show("Monitor", 10, "-"); got != "Monitor" {
		t.Errorf("show(\"Monitor\") = %q, want it drawn literally", got)
	}
}

// TestUmbrielShowWithoutState covers the headless case: with no stream behind
// it every field has to fall back to the placeholder rather than fail, because
// a widget returning an error shuts the daemon down.
func TestUmbrielShowWithoutState(t *testing.T) {
	var s umbrielSnapshot

	for field := range umbrielFields {
		if got := s.show(field, 10, "-"); got != "-" {
			t.Errorf("show(%q) = %q with no compositor, want the placeholder", field, got)
		}
	}
}

// TestUmbrielShowWithoutWindow covers an empty workspace, where the workspace
// fields still have to draw even though nothing is focused.
func TestUmbrielShowWithoutWindow(t *testing.T) {
	var s umbrielSnapshot
	if !s.apply([]byte(`{"event":"workspaces","data":[{"id":"eDP-1:1","index":1,"name":"1","layout":"master","output":"eDP-1","focused":true}]}`)) {
		t.Fatal("the workspaces event was not applied")
	}

	if got := s.show("ws-idx", 10, "-"); got != "1" {
		t.Errorf("show(\"ws-idx\") = %q, want it drawn from the workspace", got)
	}
	for _, field := range []string{"win-app", "win-title", "win-pos", "win-float"} {
		if got := s.show(field, 10, "-"); got != "-" {
			t.Errorf("show(%q) = %q with nothing focused, want the placeholder", field, got)
		}
	}
}

// TestUmbrielConcurrentApply runs the stream against the keys, which is what
// happens every second of a real session: the subscriber replaces the snapshot
// while widgets are part way through reading it. Worth pinning under -race,
// because the fields a key draws are read out of slices the next event
// replaces.
func TestUmbrielConcurrentApply(t *testing.T) {
	var s umbrielSnapshot
	done := make(chan struct{})

	// The two window lists are the same length on purpose. That is what a real
	// session streams - a title changing, not a window opening - and it is the
	// case where decoding over the previous list would reuse its memory rather
	// than allocate a new one.
	retitled := strings.ReplaceAll(umbrielFixture[1], "a terminal with a long title", "a different title entirely")

	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			for _, line := range umbrielFixture {
				s.apply([]byte(line))
			}
			s.apply([]byte(retitled))
		}
	}()

	for i := 0; i < 1000; i++ {
		for field := range umbrielFields {
			s.show(field, 10, "-")
		}
		// hold a list across some work, the way a key drawing win-pos does
		if windows, err := s.readWindows(); err == nil && len(windows) > 0 {
			umbrielColumnPos(windows, &windows[0])
		}
	}
	<-done
}

// TestUmbrielWidgetFromBundledDeck builds the widgets of the shipped umbriel
// deck from its actual TOML, which is the seam between what a deck file says
// and what a key ends up drawing.
func TestUmbrielWidgetFromBundledDeck(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join("decks", "umbriel.deck"))
	if err != nil {
		t.Fatalf("LoadConfig(decks/umbriel.deck): %v", err)
	}

	dev := &Device{Columns: 3, Rows: 3, Keys: 12, Pixels: 72, DPI: 72}
	want := map[uint8][]string{
		0: {"ws-name", "ws-idx", "ws-count"},
		1: {"win-app", "win-title", "win-pos"},
		2: {"Overview", "overview"},
		3: {"Layout", "ws-layout", "win-count"},
		4: {"Scratch", "scratchpad"},
		5: {"Monitor", "output", "output-count"},
	}

	built := 0
	for _, key := range cfg.Keys {
		if key.Widget.ID != "umbriel" {
			continue
		}
		built++

		widget, err := NewUmbrielWidget(NewBaseWidget(dev, "decks", key.Index, nil, nil, nil, nil, nil), key.Widget)
		if err != nil {
			t.Errorf("key %d: %v", key.Index, err)
			continue
		}
		w, ok := widget.(*UmbrielWidget)
		if !ok {
			t.Errorf("key %d: NewUmbrielWidget returned a %T", key.Index, widget)
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
		if w.width != umbrielDefaultWidth || w.placeholder != umbrielDefaultPlaceholder {
			t.Errorf("key %d has width %d and placeholder %q, want the defaults",
				key.Index, w.width, w.placeholder)
		}
	}

	if built != len(want) {
		t.Errorf("built %d umbriel widgets from decks/umbriel.deck, want %d", built, len(want))
	}
}

// TestUmbrielDeckFieldsExist checks that every field the shipped deck draws is
// one the integration knows how to read - a typo would otherwise be drawn as
// the literal caption it is mistaken for, on a key that looks fine until you
// notice it never changes.
func TestUmbrielDeckFieldsExist(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join("decks", "umbriel.deck"))
	if err != nil {
		t.Fatalf("LoadConfig(decks/umbriel.deck): %v", err)
	}

	// the captions the deck draws on purpose, rather than fields
	captions := map[string]bool{"Overview": true, "Layout": true, "Scratch": true, "Monitor": true}

	for _, key := range cfg.Keys {
		if key.Widget.ID != "umbriel" {
			continue
		}
		var fields []string
		if v, ok := key.Widget.Config["fields"]; ok {
			errorLog(ConfigValue(v, &fields), "reading the fields of key %d", key.Index)
		}
		for _, field := range fields {
			if _, ok := umbrielFields[field]; !ok && !captions[field] {
				t.Errorf("key %d draws '%s', which is not a field or a known caption", key.Index, field)
			}
		}
	}
}

// TestUmbrielWidgetOverrides checks that a deck can shorten the text and change
// what stands in for state umbriel does not have.
func TestUmbrielWidgetOverrides(t *testing.T) {
	dev := &Device{Columns: 3, Rows: 3, Keys: 12, Pixels: 72, DPI: 72}
	opts := WidgetConfig{
		ID: "umbriel",
		Config: map[string]interface{}{
			"fields":      []interface{}{"win-title"},
			"width":       int64(4),
			"placeholder": "",
		},
	}

	widget, err := NewUmbrielWidget(NewBaseWidget(dev, "decks", 0, nil, nil, nil, nil, nil), opts)
	if err != nil {
		t.Fatalf("NewUmbrielWidget() failed: %v", err)
	}
	w := widget.(*UmbrielWidget)

	if w.width != 4 {
		t.Errorf("width = %d, want 4", w.width)
	}
	if w.placeholder != "" {
		t.Errorf("placeholder = %q, want it empty", w.placeholder)
	}
}

// TestUmbrielLiveState reads every field from a running compositor over a real
// subscription. It is skipped where there is none, which includes CI. It only
// reads: nothing here changes what the compositor is doing.
func TestUmbrielLiveState(t *testing.T) {
	if _, err := exec.LookPath(umbrielBinary); err != nil {
		t.Skipf("no %s on PATH, no compositor to talk to", umbrielBinary)
	}

	umbrielStart()

	// the first snapshot arrives as soon as the stream opens, but the process
	// still has to start
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := umbrielState.readWorkspaces(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Skip("no state arrived, umbriel is not running here")
		}
		time.Sleep(100 * time.Millisecond)
	}

	for field := range umbrielFields {
		value, err := umbrielFields[field](&umbrielState, 10)
		if err != nil {
			t.Errorf("reading '%s' failed: %v", field, err)
			continue
		}
		t.Logf("%-12s %q", field, value)
	}

	// An action the compositor rejects exercises the whole path out - process,
	// arguments, exit status, the message on stderr - without changing
	// anything about the running session.
	err := umbrielCommand("deckedmaster-not-a-real-action")
	if err == nil {
		t.Error("umbrielCommand() accepted an action umbriel does not have")
	} else if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("umbrielCommand() failed with %q, want umbriel's own message", err)
	}

	workspaces, err := umbrielState.readWorkspaces()
	if err != nil {
		t.Fatalf("reading workspaces failed: %v", err)
	}
	if len(umbrielOutputs(workspaces)) > 1 {
		t.Skip("more than one output connected, stepping between them is not a no-op")
	}
	// With a single output the monitor dial has to reach the state for the
	// count and then do nothing at all, which is the one action path that can
	// be exercised live without moving anything.
	if err := umbrielCommand("mon-scroll +1"); err != nil {
		t.Errorf("mon-scroll on a single output failed: %v", err)
	}
}
