//go:build linux

package main

// Native support for the umbriel Wayland compositor.
//
// Where niri has to be asked for its state, umbriel offers it: `umbriel
// subscribe` streams one JSON object per line, and each event it carries is a
// full snapshot - sent the moment the stream opens, and again whenever the
// thing it describes changes. So instead of a query per key per tick behind a
// cache, which is what the niri integration next door has to do, this keeps one
// long-lived subscriber and the latest snapshot of each event in memory.
// Repainting a key reads a struct field and performs no IPC at all.
//
// Actions go the other way, through `umbriel msg`: one short-lived process per
// key press, which is what that command is for. Nothing is forked to draw a
// key, only to act on one.
//
// The subscriber finds the compositor the same way every other umbriel client
// does - $UMBRIEL_SOCKET when it is set, otherwise derived from
// $WAYLAND_DISPLAY - so there is no socket path to configure here.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// umbrielBinary is the client that both streams state and sends actions.
	umbrielBinary = "umbriel"

	// umbrielEvents are the streams a key can draw from. The "theme" event is
	// deliberately left out: nothing here draws it, and subscribing to an event
	// no field reads would only add wakeups.
	umbrielEvents = "windows,workspaces,overview,keyboard_layout,submap"

	// umbrielRetryMin and umbrielRetryMax bound the wait before a stream that
	// ended is reopened. A compositor that is restarting is back within a
	// second or two; one that is not running at all should not be respawning a
	// client process in a tight loop for the rest of the session.
	umbrielRetryMin = time.Second
	umbrielRetryMax = 30 * time.Second

	// umbrielLineMax caps one line of the stream. The windows event carries
	// every window's title, so bufio's default 64K limit is not the headroom it
	// looks like on a busy session.
	umbrielLineMax = 1 << 20

	// umbrielActionTimeout bounds one `umbriel msg`. The socket is local and
	// the client exits as soon as the compositor acknowledges the action, so
	// taking this long means nothing is listening.
	umbrielActionTimeout = 2 * time.Second
)

// errUmbrielNoState is returned by a field read before the stream has
// delivered its first snapshot, or after the compositor has gone away.
var errUmbrielNoState = errors.New("no state from umbriel, is the compositor running?")

// errUmbrielNoWorkspace is returned when no workspace holds the focus, which
// happens for a moment while the compositor is starting up.
var errUmbrielNoWorkspace = errors.New("no focused workspace")

// errUmbrielNoWindow is returned when nothing is focused - an empty workspace,
// rather than anything being wrong.
var errUmbrielNoWindow = errors.New("no active window")

// --- state ------------------------------------------------------------------

// umbrielWindow is the part of a window in the "windows" event a key draws.
//
// Active and Focused are not the same thing and the difference matters here:
// exactly one window in the whole session is Active - the one holding the
// keyboard focus, and so the one a key means by "the window" - while every
// occupied workspace has one Focused window, the one that would take the focus
// back if you switched to it.
type umbrielWindow struct {
	ID         string `json:"id"`
	AppID      string `json:"app_id"`
	Title      string `json:"title"`
	Workspace  string `json:"workspace"`
	Scratchpad string `json:"scratchpad"`
	Active     bool   `json:"active"`
	Focused    bool   `json:"focused"`
	Floating   bool   `json:"floating"`
	Urgent     bool   `json:"urgent"`
	X          int    `json:"x"`
}

// umbrielWorkspace is the part of a workspace in the "workspaces" event a key
// draws or navigates by.
//
// Focused is the workspace taking key presses; Active is the one raised on its
// output, of which there is one per output. They only come apart on a
// multi-output session - moving the pointer to another output moves Focused
// without activating anything - and it is Focused that says where you are.
type umbrielWorkspace struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Output   string `json:"output"`
	Layout   string `json:"layout"`
	Index    int64  `json:"index"`
	Active   bool   `json:"active"`
	Focused  bool   `json:"focused"`
	Occupied bool   `json:"occupied"`
	Named    bool   `json:"named"`
}

// umbrielSnapshot holds the latest value of each subscribed event. It is
// written only by the subscriber goroutine and read by every key repaint, so
// the lock is held for as short a time as it takes to copy a value out.
type umbrielSnapshot struct {
	mu sync.RWMutex

	// live reports whether the stream has delivered a snapshot that has not
	// since been invalidated by the stream ending.
	live bool

	windows      []umbrielWindow
	workspaces   []umbrielWorkspace
	overviewOpen bool
	submap       string
	kbLayouts    []string
	kbCurrent    int
}

// umbrielState is the snapshot the keys of a running deck draw from.
var umbrielState umbrielSnapshot

// umbrielStart opens the subscription on first use and leaves it open. Nothing
// closes it: it lives as long as the daemon, reopening itself whenever the
// compositor goes away and comes back.
var umbrielStart = sync.OnceFunc(func() {
	go umbrielState.run()
})

// run keeps a subscription open, reopening it when it ends. The wait between
// attempts backs off up to umbrielRetryMax so that running without umbriel -
// on a machine that has the binary but not the compositor, say - costs one
// process every half minute rather than one per second.
func (s *umbrielSnapshot) run() {
	delay := umbrielRetryMin
	for {
		delivered, err := s.stream()
		s.reset()

		if err != nil {
			verboseLog("umbriel: subscription ended: %s", err)
		}
		if delivered {
			// the compositor was there and is only now gone, so come back
			// promptly rather than at whatever the backoff had grown to
			delay = umbrielRetryMin
		}

		time.Sleep(delay)
		if delay < umbrielRetryMax {
			delay = min(2*delay, umbrielRetryMax)
		}
	}
}

// stream opens one subscription and applies events until it ends, reporting
// whether any event was applied along the way.
func (s *umbrielSnapshot) stream() (bool, error) {
	// A fixed binary with a fixed argument, so there is nothing here for a deck
	// to inject into: umbrielEvents is a constant.
	cmd := exec.Command(umbrielBinary, "subscribe", umbrielEvents) //nolint:gosec // G204: constant command and arguments

	// Its stderr is left unconnected on purpose. umbriel writes "cannot connect
	// to umbriel socket" there and exits when no compositor is running, which
	// inherited would print once per retry for the life of the daemon; the log
	// line the retry loop writes already covers it.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false, err
	}
	if err := cmd.Start(); err != nil {
		return false, err
	}
	defer func() {
		// Killing a subscriber that has already exited on its own is the
		// ordinary case here, so the failure that reports exactly that is not
		// worth a log line.
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			verboseLog("umbriel: can't stop the subscriber: %s", err)
		}
		// The exit status of a subscriber we just killed says nothing, but it
		// still has to be collected.
		_ = cmd.Wait()
	}()

	verboseLog("Subscribed to umbriel events: %s", umbrielEvents)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), umbrielLineMax)

	delivered := false
	for scanner.Scan() {
		if s.apply(scanner.Bytes()) {
			delivered = true
		}
	}
	return delivered, scanner.Err()
}

// reset drops everything the stream had delivered. State left behind by a
// compositor that has since exited would leave keys confidently drawing a
// workspace that is no longer there, so a key draws its placeholder instead
// until a new subscription delivers a fresh snapshot.
func (s *umbrielSnapshot) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.live = false
	s.windows = nil
	s.workspaces = nil
	s.overviewOpen = false
	s.submap = ""
	s.kbLayouts = nil
	s.kbCurrent = 0
}

// apply decodes one line of the stream into the snapshot, reporting whether it
// was an event we subscribed to and could read. Every event is a full
// snapshot, so each one replaces rather than amends what came before.
func (s *umbrielSnapshot) apply(line []byte) bool {
	var event struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		verboseLog("umbriel: unreadable event: %s", err)
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var err error
	switch event.Event {
	case "windows":
		// Decoded into a fresh slice rather than over the one already here:
		// unmarshalling into a non-empty slice reuses its backing array, which
		// would rewrite the windows under a key that is part way through
		// drawing the list this replaces.
		var windows []umbrielWindow
		if err = json.Unmarshal(event.Data, &windows); err == nil {
			s.windows = windows
		}
	case "workspaces":
		var workspaces []umbrielWorkspace
		if err = json.Unmarshal(event.Data, &workspaces); err == nil {
			s.workspaces = workspaces
		}
	case "overview":
		var overview struct {
			Open bool `json:"open"`
		}
		if err = json.Unmarshal(event.Data, &overview); err == nil {
			s.overviewOpen = overview.Open
		}
	case "keyboard_layout":
		var layout struct {
			Names   []string `json:"names"`
			Current int      `json:"current_index"`
		}
		if err = json.Unmarshal(event.Data, &layout); err == nil {
			s.kbLayouts, s.kbCurrent = layout.Names, layout.Current
		}
	case "submap":
		s.submap, err = umbrielSubmapName(event.Data)
	default:
		return false
	}

	if err != nil {
		verboseLog("umbriel: unreadable '%s' event: %s", event.Event, err)
		return false
	}
	s.live = true
	return true
}

// umbrielSubmapName reads the name out of a "submap" event. The event is null
// when no submap is active, which is the only shape umbriel's documentation
// pins down, so the name itself is accepted either bare or wrapped in an
// object rather than guessing at one and drawing nothing if it is the other.
func umbrielSubmapName(data json.RawMessage) (string, error) {
	if len(data) == 0 || string(data) == "null" {
		return "", nil
	}

	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		return name, nil
	}

	var wrapped struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return "", err
	}
	return wrapped.Name, nil
}

// --- reading the snapshot ----------------------------------------------------

// readWorkspaces returns the workspaces of the latest snapshot.
func (s *umbrielSnapshot) readWorkspaces() ([]umbrielWorkspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.live {
		return nil, errUmbrielNoState
	}
	return s.workspaces, nil
}

// readWindows returns the windows of the latest snapshot.
func (s *umbrielSnapshot) readWindows() ([]umbrielWindow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.live {
		return nil, errUmbrielNoState
	}
	return s.windows, nil
}

// focusedWorkspace returns the workspace a key means by "here".
func (s *umbrielSnapshot) focusedWorkspace() (*umbrielWorkspace, error) {
	workspaces, err := s.readWorkspaces()
	if err != nil {
		return nil, err
	}
	focused := umbrielFocusedIn(workspaces)
	if focused == nil {
		return nil, errUmbrielNoWorkspace
	}
	return focused, nil
}

// activeWindow returns the one window holding the keyboard focus.
func (s *umbrielSnapshot) activeWindow() (*umbrielWindow, error) {
	windows, err := s.readWindows()
	if err != nil {
		return nil, err
	}
	for i := range windows {
		if windows[i].Active {
			return &windows[i], nil
		}
	}
	return nil, errUmbrielNoWindow
}

// umbrielWorkspaces returns the workspaces of the running session, opening the
// subscription if this is the first thing to need it.
func umbrielWorkspaces() ([]umbrielWorkspace, error) {
	umbrielStart()
	return umbrielState.readWorkspaces()
}

// umbrielFocusedIn returns the workspace holding the focus, falling back to the
// one active on its output. The two only differ on a multi-output session, and
// a snapshot taken as the pointer crosses outputs can briefly have neither.
func umbrielFocusedIn(workspaces []umbrielWorkspace) *umbrielWorkspace {
	for i := range workspaces {
		if workspaces[i].Focused {
			return &workspaces[i]
		}
	}
	for i := range workspaces {
		if workspaces[i].Active {
			return &workspaces[i]
		}
	}
	return nil
}

// umbrielOnOutput returns the workspaces of one output, ordered by index -
// which is what cycling past the last one back to the first relies on.
func umbrielOnOutput(workspaces []umbrielWorkspace, output string) []umbrielWorkspace {
	var on []umbrielWorkspace
	for _, ws := range workspaces {
		if ws.Output == output {
			on = append(on, ws)
		}
	}
	sort.Slice(on, func(i, j int) bool { return on[i].Index < on[j].Index })
	return on
}

// umbrielOutputs returns the names of the connected outputs, in a stable
// order. The workspaces event covers every output, and each workspace belongs
// to exactly one, so this needs no query of its own.
func umbrielOutputs(workspaces []umbrielWorkspace) []string {
	seen := make(map[string]bool, len(workspaces))
	var outputs []string
	for _, ws := range workspaces {
		if ws.Output == "" || seen[ws.Output] {
			continue
		}
		seen[ws.Output] = true
		outputs = append(outputs, ws.Output)
	}
	sort.Strings(outputs)
	return outputs
}

// umbrielOnWorkspace returns the windows of one workspace, in the order the
// compositor lists them.
func umbrielOnWorkspace(windows []umbrielWindow, workspace string) []umbrielWindow {
	var on []umbrielWindow
	for _, win := range windows {
		if win.Workspace == workspace {
			on = append(on, win)
		}
	}
	return on
}

// umbrielColumnPos returns the position of the active window among the tiled
// windows of its workspace, counting left to right, and how many there are.
//
// Windows stacked in one column share an x, so counting distinct positions
// gives columns in the scrolling layout and left-to-right order in the others.
// Floating windows are not part of the strip and are left out of both numbers.
func umbrielColumnPos(windows []umbrielWindow, active *umbrielWindow) (pos, count int) {
	seen := make(map[int]bool, len(windows))
	var columns []int
	for _, win := range windows {
		if win.Floating || win.Workspace != active.Workspace || seen[win.X] {
			continue
		}
		seen[win.X] = true
		columns = append(columns, win.X)
	}
	sort.Ints(columns)

	for i, x := range columns {
		if x == active.X {
			return i + 1, len(columns)
		}
	}
	return 0, len(columns)
}

// --- fields -----------------------------------------------------------------

// umbrielFields maps the names an umbriel widget can draw to the state behind
// them. Anything else in a widget's "fields" list is drawn literally, which is
// how a key gets a static caption above a live value.
var umbrielFields = map[string]func(*umbrielSnapshot, int) (string, error){
	"ws-name":      (*umbrielSnapshot).showWorkspaceName,
	"ws-idx":       (*umbrielSnapshot).showWorkspaceIdx,
	"ws-count":     (*umbrielSnapshot).showWorkspaceCount,
	"ws-layout":    (*umbrielSnapshot).showWorkspaceLayout,
	"win-app":      (*umbrielSnapshot).showWindowApp,
	"win-title":    (*umbrielSnapshot).showWindowTitle,
	"win-pos":      (*umbrielSnapshot).showWindowPos,
	"win-count":    (*umbrielSnapshot).showWindowCount,
	"win-float":    (*umbrielSnapshot).showWindowFloat,
	"output":       (*umbrielSnapshot).showOutput,
	"output-count": (*umbrielSnapshot).showOutputCount,
	"overview":     (*umbrielSnapshot).showOverview,
	"submap":       (*umbrielSnapshot).showSubmap,
	"kb-layout":    (*umbrielSnapshot).showKeyboardLayout,
	"scratchpad":   (*umbrielSnapshot).showScratchpad,
}

// umbrielShow renders one field for a key, reading the session's own snapshot.
func umbrielShow(field string, width int, placeholder string) string {
	umbrielStart()
	return umbrielState.show(field, width, placeholder)
}

// show renders one field, truncating text to width characters and falling back
// to placeholder for anything umbriel does not have.
//
// It never fails. A widget that returns an error takes the whole daemon down
// with it, and a compositor that is briefly unreachable - because it has just
// been restarted, say - should leave a key showing a dash rather than do that.
func (s *umbrielSnapshot) show(field string, width int, placeholder string) string {
	render, ok := umbrielFields[field]
	if !ok {
		// not a field name, so it is the literal caption it looks like
		return field
	}

	value, err := render(s, width)
	if err != nil {
		verboseLog("umbriel: can't read '%s': %s", field, err)
		return placeholder
	}
	if value == "" {
		return placeholder
	}
	return value
}

// umbrielTrunc shortens a string to width characters, marking the cut with a
// trailing period. Key screens are around 60 pixels wide, so a window title
// that is not cut down renders as an unreadable smudge.
func umbrielTrunc(s string, width int) string {
	if width <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	return string(runes[:width-1]) + "."
}

func (s *umbrielSnapshot) showWorkspaceName(width int) (string, error) {
	ws, err := s.focusedWorkspace()
	if err != nil {
		return "", err
	}
	if ws.Name == "" {
		return "ws", nil
	}
	return umbrielTrunc(ws.Name, width), nil
}

func (s *umbrielSnapshot) showWorkspaceIdx(int) (string, error) {
	ws, err := s.focusedWorkspace()
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(ws.Index, 10), nil
}

// showWorkspaceCount renders the focused workspace's position in the list of
// workspaces on its own output, as "<index>/<total>".
func (s *umbrielSnapshot) showWorkspaceCount(int) (string, error) {
	workspaces, err := s.readWorkspaces()
	if err != nil {
		return "", err
	}
	focused := umbrielFocusedIn(workspaces)
	if focused == nil {
		return "", errUmbrielNoWorkspace
	}
	return fmt.Sprintf("%d/%d", focused.Index, len(umbrielOnOutput(workspaces, focused.Output))), nil
}

// showWorkspaceLayout renders the layout mode of the focused workspace:
// scrolling, dwindle or master.
func (s *umbrielSnapshot) showWorkspaceLayout(width int) (string, error) {
	ws, err := s.focusedWorkspace()
	if err != nil {
		return "", err
	}
	return umbrielTrunc(ws.Layout, width), nil
}

func (s *umbrielSnapshot) showWindowTitle(width int) (string, error) {
	window, err := s.activeWindow()
	if err != nil {
		return "", err
	}
	return umbrielTrunc(window.Title, width), nil
}

func (s *umbrielSnapshot) showWindowApp(width int) (string, error) {
	window, err := s.activeWindow()
	if err != nil {
		return "", err
	}

	// app ids are usually reverse-DNS; the last component is the useful part
	app := window.AppID
	if dot := strings.LastIndex(app, "."); dot >= 0 {
		app = app[dot+1:]
	}
	return umbrielTrunc(app, width), nil
}

// showWindowPos renders the position of the active window's column in its
// workspace, as "<column>/<columns>" - the readout for a dial bound to
// win-scroll.
func (s *umbrielSnapshot) showWindowPos(int) (string, error) {
	windows, err := s.readWindows()
	if err != nil {
		return "", err
	}
	active, err := s.activeWindow()
	if err != nil {
		return "", err
	}
	if active.Floating {
		// a floating window is not in the strip at all
		return "", nil
	}

	pos, count := umbrielColumnPos(windows, active)
	if pos == 0 {
		return "", nil
	}
	return fmt.Sprintf("%d/%d", pos, count), nil
}

// showWindowCount renders how many windows are on the focused workspace.
func (s *umbrielSnapshot) showWindowCount(int) (string, error) {
	ws, err := s.focusedWorkspace()
	if err != nil {
		return "", err
	}
	windows, err := s.readWindows()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d win", len(umbrielOnWorkspace(windows, ws.ID))), nil
}

// showWindowFloat renders whether the active window floats above the layout or
// is tiled into it - the readout for a key bound to window-toggle-floating.
func (s *umbrielSnapshot) showWindowFloat(int) (string, error) {
	window, err := s.activeWindow()
	if err != nil {
		return "", err
	}
	if window.Floating {
		return "float", nil
	}
	return "tile", nil
}

func (s *umbrielSnapshot) showOutput(width int) (string, error) {
	ws, err := s.focusedWorkspace()
	if err != nil {
		return "", err
	}
	return umbrielTrunc(ws.Output, width), nil
}

func (s *umbrielSnapshot) showOutputCount(int) (string, error) {
	workspaces, err := s.readWorkspaces()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d mon", len(umbrielOutputs(workspaces))), nil
}

func (s *umbrielSnapshot) showOverview(int) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.live {
		return "", errUmbrielNoState
	}
	if s.overviewOpen {
		return "OPEN", nil
	}
	return "closed", nil
}

// showSubmap renders the active keybind submap, or nothing at all when the
// session is in its default context - which is most of the time, so a key
// drawing this shows its placeholder until a submap is entered.
func (s *umbrielSnapshot) showSubmap(width int) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.live {
		return "", errUmbrielNoState
	}
	return umbrielTrunc(s.submap, width), nil
}

func (s *umbrielSnapshot) showKeyboardLayout(width int) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.live {
		return "", errUmbrielNoState
	}
	if s.kbCurrent < 0 || s.kbCurrent >= len(s.kbLayouts) {
		return "", nil
	}
	return umbrielTrunc(s.kbLayouts[s.kbCurrent], width), nil
}

// showScratchpad renders how many windows are currently stashed in a
// scratchpad - the readout for a key bound to scratchpad-toggle.
func (s *umbrielSnapshot) showScratchpad(int) (string, error) {
	windows, err := s.readWindows()
	if err != nil {
		return "", err
	}

	stashed := 0
	for _, win := range windows {
		if win.Scratchpad != "" {
			stashed++
		}
	}
	if stashed == 0 {
		return "", nil
	}
	return strconv.Itoa(stashed), nil
}

// --- actions ----------------------------------------------------------------

// umbrielCommand performs an "umbriel" action bound to a key. cmd is one of:
//
//   - the name of an umbriel action, spelled the way `umbriel msg` spells it,
//     with any argument after a colon: "window-toggle-maximize",
//     "workspace-switch:3", "spawn:foot", ...
//   - one of the compound commands below, which do what a single umbriel
//     action cannot.
//
// The compound commands are:
//
//	ws-cycle          next workspace on this output, wrapping past the last
//	ws-scroll -1|+1   previous/next workspace, for a dial
//	win-scroll -1|+1  previous/next window in layout order, for a dial
//	mon-scroll -1|+1  previous/next output, a no-op with a single output
//	mon-next          next output, a no-op with a single output
func umbrielCommand(cmd string) error {
	umbrielStart()

	fields := strings.Fields(strings.TrimSpace(cmd))
	if len(fields) == 0 {
		return errors.New("no umbriel command given")
	}
	name, args := fields[0], fields[1:]

	switch name {
	case "ws-cycle":
		return umbrielCycleWorkspace()
	case "ws-scroll":
		return umbrielAction(umbrielPick(args, "workspace-previous", "workspace-next"))
	case "win-scroll":
		return umbrielAction(umbrielPick(args, "window-focus-previous", "window-focus-next"))
	case "mon-scroll":
		return umbrielMonitor(umbrielPick(args, "output-focus-previous", "output-focus-next"))
	case "mon-next":
		return umbrielMonitor("output-focus-next")
	}
	return umbrielAction(fields...)
}

// umbrielAction runs `umbriel msg` with the given action. The action and its
// arguments are passed as separate words, which covers both spellings umbriel
// accepts: "spawn:foot --title x" and "spawn foot --title x" alike.
func umbrielAction(action ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), umbrielActionTimeout)
	defer cancel()

	// The action comes from a deck file, which is configuration the user wrote,
	// and it is passed as arguments to a fixed binary rather than to a shell,
	// so there is no command here for it to break out of.
	cmd := exec.CommandContext(ctx, umbrielBinary, append([]string{"msg"}, action...)...) //nolint:gosec // G204: fixed binary, arguments are not shell-interpreted

	// umbriel reports a rejected action on stderr ("error: unknown action: x")
	// and exits non-zero, which is worth putting in the log as it stands.
	output, err := cmd.CombinedOutput()
	if err != nil {
		// The daemon reaps every child it has on SIGCHLD, so there is a window
		// in which the status of this one is collected before we ask for it.
		// Go's own wait wins that race in practice, but losing it says nothing
		// about whether the action was performed - and the action has gone out
		// either way - so it is not reported as a failure.
		if errors.Is(err, syscall.ECHILD) {
			verboseLog("umbriel: '%s' was reaped before its status could be read", strings.Join(action, " "))
			return nil
		}
		if reason := strings.TrimSpace(string(output)); reason != "" {
			return fmt.Errorf("%s: %s", err, reason)
		}
		return err
	}
	return nil
}

// umbrielMonitor performs an action that steps between outputs. It is a no-op
// on a single-output session: without this guard a dial that cannot go
// anywhere spends a process per detent to do nothing.
//
// A session whose state has not arrived yet is not second-guessed - the action
// is sent, because swallowing a key press is worse than a wasted one.
func umbrielMonitor(action string) error {
	workspaces, err := umbrielWorkspaces()
	if err == nil && len(umbrielOutputs(workspaces)) < 2 {
		return nil
	}
	return umbrielAction(action)
}

// umbrielCycleWorkspace focuses the next workspace on the focused output,
// wrapping past the last one back to the first - which is what this adds over
// umbriel's own workspace-next, as that one stops at the end of the list.
func umbrielCycleWorkspace() error {
	workspaces, err := umbrielWorkspaces()
	if err != nil {
		return err
	}
	focused := umbrielFocusedIn(workspaces)
	if focused == nil {
		return errUmbrielNoWorkspace
	}

	onOutput := umbrielOnOutput(workspaces, focused.Output)
	for i, ws := range onOutput {
		if ws.ID != focused.ID {
			continue
		}
		next := onOutput[(i+1)%len(onOutput)]
		return umbrielAction("workspace-switch:" + umbrielWorkspaceReference(next))
	}
	return nil
}

// umbrielWorkspaceReference spells a workspace the way workspace-switch takes
// it: "<workspace>/<output>", with a named workspace quoted so that a name
// that looks like a number is not taken for an index.
func umbrielWorkspaceReference(ws umbrielWorkspace) string {
	name := ws.Name
	if ws.Named {
		name = strconv.Quote(name)
	}
	return name + "/" + ws.Output
}

// umbrielPick returns negative when the first argument is a negative number,
// the way a dial reports a counter-clockwise detent, and positive otherwise.
func umbrielPick(args []string, negative, positive string) string {
	if len(args) > 0 && strings.HasPrefix(args[0], "-") {
		return negative
	}
	return positive
}
