//go:build linux

package main

// Native support for the niri Wayland compositor.
//
// niri exposes an IPC socket - the path is in $NIRI_SOCKET, exported into
// every process the compositor spawns - which speaks one line of JSON per
// request and answers with one line of JSON. Everything a key needs, both the
// state it draws and the actions it triggers, goes over that socket directly,
// so no `niri msg` process is ever forked to paint a key.
//
// Replies are cached in $XDG_RUNTIME_DIR/niri-deck-<uid>/, one file per query,
// named after the `niri msg` subcommand that returns the same JSON. The cache
// exists because keys refresh independently but ask the same questions: the
// workspace key alone asks three per tick. It is a file rather than a map so
// that the state a key is drawing can be looked at from a shell, and so that
// the deck and a `niri msg` wrapper script can share one cache.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// niriSocketEnv names the environment variable holding the path of the
	// compositor's IPC socket.
	niriSocketEnv = "NIRI_SOCKET"

	// niriTimeout bounds a single round trip. The socket is local and the
	// answers are small, so a reply that takes this long means the compositor
	// is wedged and the key is better off drawing its placeholder.
	niriTimeout = 2 * time.Second

	// niriCacheTTL is how long one reply is shared between keys. A key can be
	// configured to refresh faster than this, but it will redraw the same
	// values until the cache expires.
	niriCacheTTL = time.Second
)

// errNiriNoWorkspace is returned when niri reports no focused workspace,
// which happens for a moment while the compositor is starting up.
var errNiriNoWorkspace = errors.New("no focused workspace")

// niriRuntimeDir resolves the directory replies are cached in, creating it on
// first use. It returns an empty path when the directory cannot be created,
// in which case every query goes to the compositor.
var niriRuntimeDir = sync.OnceValue(func() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}

	dir := filepath.Join(base, fmt.Sprintf("niri-deck-%d", os.Getuid()))
	if err := os.MkdirAll(dir, DirMode); err != nil {
		errorLog(err, "niri: can't create the runtime directory %s", dir)
		return ""
	}
	verboseLog("Caching niri state in %s", dir)
	return dir
})

// niriCacheMutex serializes access to the cache. Widgets are repainted one at
// a time, but an action can run while the REST API is repainting a key.
var niriCacheMutex sync.Mutex

// niriRequest performs one request/response round trip and returns the
// contents of the reply's "Ok" field.
func niriRequest(request interface{}) (json.RawMessage, error) {
	socket := os.Getenv(niriSocketEnv)
	if socket == "" {
		return nil, fmt.Errorf("$%s is not set, is niri running?", niriSocketEnv)
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	// The path comes from the environment, but it is a unix socket the
	// compositor that spawned us exported, not a network address, so there is
	// nothing here for a request to be forged against.
	conn, err := net.DialTimeout("unix", socket, niriTimeout) //nolint:gosec // G704: $NIRI_SOCKET is a local socket, not a URL
	if err != nil {
		return nil, err
	}
	defer conn.Close() //nolint:errcheck

	if err := conn.SetDeadline(time.Now().Add(niriTimeout)); err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return nil, err
	}

	// The reply is a single line. One that ends at EOF rather than at a
	// newline is still complete, so only an empty read is an error.
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(bytes.TrimSpace(line)) == 0 {
		return nil, errors.New("empty reply")
	}

	var reply struct {
		Ok  json.RawMessage `json:"Ok"`
		Err *string         `json:"Err"`
	}
	if err := json.Unmarshal(line, &reply); err != nil {
		return nil, fmt.Errorf("unparseable reply %s: %w", bytes.TrimSpace(line), err)
	}
	if reply.Err != nil {
		return nil, fmt.Errorf("niri rejected %s: %s", payload, *reply.Err)
	}
	return reply.Ok, nil
}

// niriPayload strips the single-field wrapper niri puts around a query result
// ({"Workspaces": [...]}), leaving the JSON `niri msg --json workspaces`
// prints.
func niriPayload(ok json.RawMessage) (json.RawMessage, error) {
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(ok, &wrapper); err != nil {
		return nil, err
	}
	if len(wrapper) != 1 {
		return nil, fmt.Errorf("expected a reply with one field, got %d", len(wrapper))
	}
	for _, payload := range wrapper {
		return payload, nil
	}
	return nil, errors.New("empty reply")
}

// niriQuery decodes the result of a query into dst. name is the `niri msg`
// subcommand the request corresponds to, and doubles as the cache file name.
//
// A reply younger than niriCacheTTL is re-read from the cache instead of being
// asked for again. A failed query is neither cached nor allowed to clear the
// cache, so a compositor that has just restarted is picked up on the next tick
// rather than remembered as broken.
func niriQuery(name string, request interface{}, dst interface{}) error {
	// Without a socket there is no compositor to refresh the cache from, and
	// serving state left behind by one that has since exited would leave keys
	// confidently showing a workspace that is no longer there.
	if os.Getenv(niriSocketEnv) == "" {
		return fmt.Errorf("$%s is not set, is niri running?", niriSocketEnv)
	}

	niriCacheMutex.Lock()
	defer niriCacheMutex.Unlock()

	path := niriCachePath(name)
	if payload, ok := niriCached(path); ok {
		if err := json.Unmarshal(payload, dst); err == nil {
			return nil
		}
		// a cache file we can't read is worth one fresh query
	}

	ok, err := niriRequest(request)
	if err != nil {
		return err
	}
	payload, err := niriPayload(ok)
	if err != nil {
		return err
	}

	if path != "" {
		errorLog(os.WriteFile(path, payload, FileMode), "niri: can't cache %s", path)
	}
	return json.Unmarshal(payload, dst)
}

func niriCachePath(name string) string {
	dir := niriRuntimeDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, name+".json")
}

func niriCached(path string) (json.RawMessage, bool) {
	if path == "" {
		return nil, false
	}

	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 || time.Since(info.ModTime()) >= niriCacheTTL {
		return nil, false
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return payload, true
}

// niriInvalidate drops the cache, so that the keys reading the state an action
// has just changed repaint from the new state rather than from the old one.
func niriInvalidate() {
	niriCacheMutex.Lock()
	defer niriCacheMutex.Unlock()

	dir := niriRuntimeDir()
	if dir == "" {
		return
	}
	cached, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return
	}
	for _, path := range cached {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			errorLog(err, "niri: can't drop the cached %s", filepath.Base(path))
		}
	}
}

// --- state ------------------------------------------------------------------

// niriWorkspace is the part of niri's Workspace a key draws or navigates by.
// Unnamed workspaces have a null name, which decodes to an empty string.
type niriWorkspace struct {
	Idx       int64  `json:"idx"`
	Name      string `json:"name"`
	Output    string `json:"output"`
	IsFocused bool   `json:"is_focused"`
}

// niriWindow is the part of niri's Window a key draws.
type niriWindow struct {
	Title       string `json:"title"`
	AppID       string `json:"app_id"`
	WorkspaceID int64  `json:"workspace_id"`
	Layout      struct {
		// Column and row of the window in the workspace's scrolling layout.
		// Null for floating windows, which are not part of it.
		PosInScrollingLayout []int64 `json:"pos_in_scrolling_layout"`
	} `json:"layout"`
}

// niriOutput is the part of niri's Output a key draws.
type niriOutput struct {
	Name string `json:"name"`
}

func niriWorkspaces() ([]niriWorkspace, error) {
	var workspaces []niriWorkspace
	if err := niriQuery("workspaces", "Workspaces", &workspaces); err != nil {
		return nil, err
	}
	return workspaces, nil
}

// niriFocusedWindow returns nil without an error when nothing is focused.
func niriFocusedWindow() (*niriWindow, error) {
	var window *niriWindow
	if err := niriQuery("focused-window", "FocusedWindow", &window); err != nil {
		return nil, err
	}
	return window, nil
}

func niriWindows() ([]niriWindow, error) {
	var windows []niriWindow
	if err := niriQuery("windows", "Windows", &windows); err != nil {
		return nil, err
	}
	return windows, nil
}

// niriFocusedOutput returns nil without an error when no output is focused.
func niriFocusedOutput() (*niriOutput, error) {
	var output *niriOutput
	if err := niriQuery("focused-output", "FocusedOutput", &output); err != nil {
		return nil, err
	}
	return output, nil
}

// niriOutputs returns the connected outputs, keyed by name.
func niriOutputs() (map[string]niriOutput, error) {
	var outputs map[string]niriOutput
	if err := niriQuery("outputs", "Outputs", &outputs); err != nil {
		return nil, err
	}
	return outputs, nil
}

func niriOverviewOpen() (bool, error) {
	var state struct {
		IsOpen bool `json:"is_open"`
	}
	if err := niriQuery("overview-state", "OverviewState", &state); err != nil {
		return false, err
	}
	return state.IsOpen, nil
}

// niriFocusedIn returns the focused workspace out of a list, or nil.
func niriFocusedIn(workspaces []niriWorkspace) *niriWorkspace {
	for i := range workspaces {
		if workspaces[i].IsFocused {
			return &workspaces[i]
		}
	}
	return nil
}

// niriFocusedWorkspace returns the focused workspace on any output.
func niriFocusedWorkspace() (*niriWorkspace, error) {
	workspaces, err := niriWorkspaces()
	if err != nil {
		return nil, err
	}
	focused := niriFocusedIn(workspaces)
	if focused == nil {
		return nil, errNiriNoWorkspace
	}
	return focused, nil
}

// niriOnOutput returns the workspaces of one output, ordered by index. niri
// always keeps a trailing empty workspace, so the list is one longer than the
// number of workspaces that have actually been used.
func niriOnOutput(workspaces []niriWorkspace, output string) []niriWorkspace {
	var on []niriWorkspace
	for _, ws := range workspaces {
		if ws.Output == output {
			on = append(on, ws)
		}
	}
	sort.Slice(on, func(i, j int) bool { return on[i].Idx < on[j].Idx })
	return on
}

// --- fields -----------------------------------------------------------------

// niriFields maps the names a niri widget can draw to the state behind them.
// Anything else in a widget's "fields" list is drawn literally, which is how a
// key gets a static caption above a live value.
var niriFields = map[string]func(width int) (string, error){
	"ws-name":      niriShowWorkspaceName,
	"ws-idx":       niriShowWorkspaceIdx,
	"ws-count":     niriShowWorkspaceCount,
	"win-app":      niriShowWindowApp,
	"win-title":    niriShowWindowTitle,
	"win-pos":      niriShowWindowPos,
	"output":       niriShowOutput,
	"output-count": niriShowOutputCount,
	"overview":     niriShowOverview,
}

// niriShow renders one field for a key, truncating text to width characters
// and falling back to placeholder for anything niri does not have.
//
// It never fails. A widget that returns an error takes the whole daemon down
// with it, and a compositor that is briefly unreachable - because it has just
// been restarted, say - should leave a key showing a dash rather than do that.
func niriShow(field string, width int, placeholder string) string {
	show, ok := niriFields[field]
	if !ok {
		// not a field name, so it is the literal caption it looks like
		return field
	}

	value, err := show(width)
	if err != nil {
		verboseLog("niri: can't read '%s': %s", field, err)
		return placeholder
	}
	if value == "" {
		return placeholder
	}
	return value
}

// niriTrunc shortens a string to width characters, marking the cut with a
// trailing period. Key screens are around 60 pixels wide, so a window title
// that is not cut down renders as an unreadable smudge.
func niriTrunc(s string, width int) string {
	if width <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	return string(runes[:width-1]) + "."
}

func niriShowWorkspaceIdx(int) (string, error) {
	ws, err := niriFocusedWorkspace()
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(ws.Idx, 10), nil
}

func niriShowWorkspaceName(width int) (string, error) {
	ws, err := niriFocusedWorkspace()
	if err != nil {
		return "", err
	}
	if ws.Name == "" {
		return "ws", nil
	}
	return niriTrunc(ws.Name, width), nil
}

// niriShowWorkspaceCount renders the focused workspace's position in the list
// of workspaces on its own output, as "<idx>/<total>".
func niriShowWorkspaceCount(int) (string, error) {
	workspaces, err := niriWorkspaces()
	if err != nil {
		return "", err
	}
	focused := niriFocusedIn(workspaces)
	if focused == nil {
		return "", errNiriNoWorkspace
	}
	return fmt.Sprintf("%d/%d", focused.Idx, len(niriOnOutput(workspaces, focused.Output))), nil
}

func niriShowWindowTitle(width int) (string, error) {
	window, err := niriFocusedWindow()
	if err != nil || window == nil {
		return "", err
	}
	return niriTrunc(window.Title, width), nil
}

func niriShowWindowApp(width int) (string, error) {
	window, err := niriFocusedWindow()
	if err != nil || window == nil {
		return "", err
	}

	// app ids are usually reverse-DNS; the last component is the useful part
	app := window.AppID
	if dot := strings.LastIndex(app, "."); dot >= 0 {
		app = app[dot+1:]
	}
	return niriTrunc(app, width), nil
}

// niriShowWindowPos renders the position of the focused column in its
// workspace's scrolling layout, as "<column>/<columns>" - the readout for a
// dial bound to win-scroll.
func niriShowWindowPos(int) (string, error) {
	window, err := niriFocusedWindow()
	if err != nil || window == nil {
		return "", err
	}
	if len(window.Layout.PosInScrollingLayout) == 0 {
		// a floating window is not in the scrolling layout at all
		return "", nil
	}

	windows, err := niriWindows()
	if err != nil {
		return "", err
	}

	columns := make(map[int64]bool)
	for _, other := range windows {
		if other.WorkspaceID != window.WorkspaceID || len(other.Layout.PosInScrollingLayout) == 0 {
			continue
		}
		columns[other.Layout.PosInScrollingLayout[0]] = true
	}
	return fmt.Sprintf("%d/%d", window.Layout.PosInScrollingLayout[0], len(columns)), nil
}

func niriShowOutput(width int) (string, error) {
	output, err := niriFocusedOutput()
	if err != nil || output == nil {
		return "", err
	}
	return niriTrunc(output.Name, width), nil
}

func niriShowOutputCount(int) (string, error) {
	outputs, err := niriOutputs()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d mon", len(outputs)), nil
}

func niriShowOverview(int) (string, error) {
	open, err := niriOverviewOpen()
	if err != nil {
		return "", err
	}
	if open {
		return "OPEN", nil
	}
	return "closed", nil
}

// --- actions ----------------------------------------------------------------

// niriCommand performs a "niri" action bound to a key. cmd is one of:
//
//   - the name of a niri action, spelled the way `niri msg action` spells it:
//     "maximize-column", "toggle-overview", "focus-workspace 3", ...
//   - one of the compound commands below, which do what a single niri action
//     cannot;
//   - raw JSON for niri's Action enum, for an action taking arguments this
//     does not otherwise cover: {"FocusWorkspace":{"reference":{"Id":5}}}.
//
// The compound commands are:
//
//	ws-cycle          next workspace on this output, wrapping past the last
//	ws-scroll -1|+1   one workspace up/down, for a dial
//	win-scroll -1|+1  one column left/right, for a dial
//	mon-scroll -1|+1  previous/next monitor, a no-op with a single output
//	mon-next          next monitor, a no-op with a single output
func niriCommand(cmd string) error {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, "{") {
		return niriPerform(json.RawMessage(cmd))
	}

	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return errors.New("no niri command given")
	}
	name, args := fields[0], fields[1:]

	switch name {
	case "ws-cycle":
		return niriCycleWorkspace()
	case "ws-scroll":
		return niriAction(niriPick(args, "focus-workspace-up", "focus-workspace-down"), nil)
	case "win-scroll":
		return niriAction(niriPick(args, "focus-column-left", "focus-column-right"), nil)
	case "mon-scroll":
		return niriMonitor(niriPick(args, "focus-monitor-previous", "focus-monitor-next"))
	case "mon-next":
		return niriMonitor("focus-monitor-next")
	}
	return niriAction(name, args)
}

// niriPerform sends an action and drops the cache, so that a key reading the
// state it changed reflects the press on its very next refresh.
func niriPerform(action interface{}) error {
	if _, err := niriRequest(niriActionRequest(action)); err != nil {
		return err
	}
	niriInvalidate()
	return nil
}

// niriActionRequest wraps an action in the request niri expects.
func niriActionRequest(action interface{}) map[string]interface{} {
	return map[string]interface{}{"Action": action}
}

// niriAction sends a niri action by name, converting the kebab-case spelling
// used on the command line to the CamelCase variant name the IPC uses. Actions
// that take no arguments - which is nearly all of them, and all the ones a key
// normally binds - need nothing else.
//
// "focus-workspace" is special-cased because cycling workspaces needs it and
// its argument is a nested reference rather than a plain value. Anything else
// taking arguments can be sent as raw JSON.
func niriAction(name string, args []string) error {
	action, err := niriActionPayload(name, args)
	if err != nil {
		return err
	}
	return niriPerform(action)
}

// niriActionPayload builds the variant of niri's Action enum an action name
// stands for.
func niriActionPayload(name string, args []string) (interface{}, error) {
	if name == "focus-workspace" && len(args) == 1 {
		return map[string]interface{}{
			"FocusWorkspace": map[string]interface{}{
				"reference": niriWorkspaceReference(args[0]),
			},
		}, nil
	}
	if len(args) > 0 {
		return nil, fmt.Errorf("niri action '%s' takes no arguments here, pass raw JSON for one that does", name)
	}
	return map[string]interface{}{niriCamelCase(name): struct{}{}}, nil
}

// niriMonitor performs an action that steps between monitors. It is a no-op on
// a single-output machine: without this guard niri logs a warning for every
// detent of a dial that cannot go anywhere.
func niriMonitor(action string) error {
	outputs, err := niriOutputs()
	if err != nil {
		return err
	}
	if len(outputs) < 2 {
		return nil
	}
	return niriAction(action, nil)
}

// niriCycleWorkspace focuses the next workspace on the focused output,
// wrapping past the last one back to the first. niri has no action for this:
// focus-workspace-down stops dead at the end of the list, which is wrong for a
// key whose whole job is to cycle.
func niriCycleWorkspace() error {
	workspaces, err := niriWorkspaces()
	if err != nil {
		return err
	}
	focused := niriFocusedIn(workspaces)
	if focused == nil {
		return errNiriNoWorkspace
	}

	onOutput := niriOnOutput(workspaces, focused.Output)
	for i, ws := range onOutput {
		if ws.Idx != focused.Idx {
			continue
		}
		next := onOutput[(i+1)%len(onOutput)]
		return niriAction("focus-workspace", []string{strconv.FormatInt(next.Idx, 10)})
	}
	return nil
}

// niriPick returns negative when the first argument is a negative number, the
// way a dial reports a counter-clockwise detent, and positive otherwise.
func niriPick(args []string, negative, positive string) string {
	if len(args) > 0 && strings.HasPrefix(args[0], "-") {
		return negative
	}
	return positive
}

// niriWorkspaceReference builds the reference a workspace action takes,
// treating a number as an index and anything else as a workspace name.
func niriWorkspaceReference(ref string) map[string]interface{} {
	if idx, err := strconv.ParseUint(ref, 10, 64); err == nil {
		return map[string]interface{}{"Index": idx}
	}
	return map[string]interface{}{"Name": ref}
}

// niriCamelCase converts an action name from the kebab-case spelling used on
// the command line ("toggle-windowed-fullscreen") to the variant name the IPC
// expects ("ToggleWindowedFullscreen"). A name already spelled that way is
// passed through unchanged.
func niriCamelCase(name string) string {
	var b strings.Builder
	for _, word := range strings.Split(name, "-") {
		if word == "" {
			continue
		}
		b.WriteString(strings.ToUpper(word[:1]))
		b.WriteString(word[1:])
	}
	return b.String()
}
