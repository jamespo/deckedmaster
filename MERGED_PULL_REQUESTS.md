# Merged pull requests

This branch integrates every pull request that was open against
[muesli/deckmaster](https://github.com/muesli/deckmaster) at the time of the
merge. It is a **integration branch**, not an upstream release: several PRs
overlapped and had to be reconciled, so the behaviour of a few of them differs
slightly from what their authors originally proposed. Everything that changed is
listed below.

Baseline: `9fd738d` ("Update README.md with link to new deck").

## Summary

| PR | Title | Author | Status |
| --- | --- | --- | --- |
| [#147](https://github.com/muesli/deckmaster/pull/147) | Support for mute and audio stream changing buttons | tvidal-net | Merged, with adaptations |
| [#135](https://github.com/muesli/deckmaster/pull/135) | Temperature sensor widget | unterwegi | Merged |
| [#134](https://github.com/muesli/deckmaster/pull/134) | Solid key background & blending | TwoD | Merged, re-applied onto #147 |
| [#133](https://github.com/muesli/deckmaster/pull/133) | Initial implementation of REST based API | TwoD | Merged, with adaptations |
| [#132](https://github.com/muesli/deckmaster/pull/132) | Fix DBus method call example | HoodedDeath | Merged |
| [#130](https://github.com/muesli/deckmaster/pull/130) | Bump golang.org/x/image to 0.11.0 | dependabot | Merged, superseded by #147 |
| [#128](https://github.com/muesli/deckmaster/pull/128) | Update README.md for new mini | jamespo | Merged |
| [#125](https://github.com/muesli/deckmaster/pull/125) | Add spotify widget | introini | Merged |
| [#124](https://github.com/muesli/deckmaster/pull/124) | Bump BurntSushi/toml to 1.3.2 | dependabot | Merged, superseded by #147 |
| [#121](https://github.com/muesli/deckmaster/pull/121) | Bump bendahl/uinput to 1.6.2 | dependabot | Merged, superseded by #147 |
| [#111](https://github.com/muesli/deckmaster/pull/111) | Add borderColor attribute to widget_top | pfsmorigo | Merged |
| [#105](https://github.com/muesli/deckmaster/pull/105) | Media player (DBus) widget and commands | DemonTPx | Merged, with adaptations |
| [#99](https://github.com/muesli/deckmaster/pull/99) | Move background/parent options above widgets | HoodedDeath | Merged |
| [#98](https://github.com/muesli/deckmaster/pull/98) | Implement ImageCommand | dgalli1 | Merged |
| [#93](https://github.com/muesli/deckmaster/pull/93) | Pulse Audio Control Widget | Someone0nEarth | Merged |
| [#87](https://github.com/muesli/deckmaster/pull/87) | Widget to toggle mute of pulseaudio default source | lnqs | Merged, **widget id renamed** |
| [#86](https://github.com/muesli/deckmaster/pull/86) | Recommend UACCESS tag instead of plugdev | lnqs | Merged |
| [#80](https://github.com/muesli/deckmaster/pull/80) | Don't trigger actions before key release | muesli | Merged, kept over #147 |
| [#67](https://github.com/muesli/deckmaster/pull/67) | Timer widget | norrs | **Not applied** — superseded by #66 |
| [#66](https://github.com/muesli/deckmaster/pull/66) | Implement new timer widget | zMoooooritz | Merged, minus unrelated changes |
| [#50](https://github.com/muesli/deckmaster/pull/50) | Automatic config reloading | dolfje | Merged, reworked |

20 of the 21 open PRs are applied. The one exception, #67, is a duplicate of
#66 — see below.

## Conflicts that required a decision

### #66 vs #67 — two timer widgets

Both PRs add a `widget_timer.go` that registers the widget id `timer`, with the
same config keys (`times`, `font`, `color`, `underflow`, `underflowColor`) and
the same press/hold semantics. They cannot coexist: Go rejects duplicate cases
in the `NewWidget` switch, and the two would claim the same id regardless.

**Resolution:** #66 is applied. It is a strict superset of #67 (it adds the
`adaptive` option) and was updated more recently. #67 is not applied because
doing so would only mean registering a second, less capable implementation of
the same widget under a made-up id.

#66 also rewrote `.goreleaser.yml`, commenting out the `nfpms` section that
produces the apk/deb/rpm packages the README advertises. That looked like local
build scaffolding rather than an intended upstream change, so **only the timer
widget from #66 was taken**; `.goreleaser.yml` is untouched.

### #87 vs #147 — two `mute` widgets and two `pulseaudio.go`

Both PRs add a file called `pulseaudio.go`, built on different libraries
(#87 uses `jfreymuth/pulse`, #147 uses `tvidal-net/pulseaudio`), and both
register a widget with the id `mute`.

**Resolution:** both implementations are kept, because they do different things.

* #147's client stays in `pulseaudio.go`; #87's moved to
  `pulseaudio_microphone.go`. The two share no type names, so they coexist.
* #147 keeps the `mute` id, which toggles the default **sink** (speakers).
* #87's widget is registered as **`microphoneMute`**, which toggles the default
  **source** (microphone). Its config keys (`icon`, `iconMute`) are unchanged.

> **Migration:** if you used `id = "mute"` with the `icon`/`iconMute` options
> from #87, rename it to `id = "microphoneMute"`. The bundled
> `decks/main.deck` has been updated accordingly. Leaving it as `mute` will
> silently give you the sink-mute widget instead, which ignores those options.

### #80 vs #147 — long-press handling

#80 (by the maintainer) removed the goroutine that fires a long-press action
while the key is still held, so actions only trigger on release. #147 was
branched before that and reintroduced the goroutine.

**Resolution:** #80's behaviour is kept. Long presses fire on release.

### #105, #147 — DBus v4 to v5

#105 and #147 both move from `github.com/godbus/dbus` to the maintained
`github.com/godbus/dbus/v5`. The migration is applied tree-wide. #147 also
routes all DBus calls through a new `dbus.go` (`CallDBus`), so the global
`dbusConn` from the old code is gone.

### #133 vs #147 — incompatible `Deck` structures

This was the most invasive conflict. #147 rewrites `Deck` to use unexported
fields, a `map[uint8]Widget` for widgets, per-window overrides, and a
range-over-func `Widgets` iterator. #133's REST API was written against the old
exported `File`/`Config`/`Background`/`Widgets []Widget` layout.

**Resolution:** #147's structure is the base; #133 was adapted onto it.

* `Deck` regained a `dev` field, which the API needs.
* `api.go` now uses `deck.file`, `deck.config`, `deck.background`, iterates
  `deck.Widgets` with a single loop variable, and indexes `deck.widgets`.
* #133's `setDeckConfig` was split into `Deck.applyConfig` (deck-level settings)
  and `LoadWidgets` (rebuilds all widgets). The API calls both, so a config
  change still applies without a restart.
* `ActionConfig.DBus` became `*DBusConfig` (from #133, for `omitempty` JSON);
  the call site in `deck.go` is nil-guarded.
* Fixed a typo in #133: `KeyConfig.Name` was tagged `tomk:"name"`, so the field
  could never be set from a TOML file. It is now `toml:"name"`.

### #135 vs #147 — gopsutil version

#135 needs `gopsutil/v3` for the sensors API; #147 pinned the older
`gopsutil` v2 path. `gopsutil/v3` is used, since #147 does not depend on the
older one.

### #50 — config autoreload, reworked

The original implementation had defects that would have shipped real bugs:

* `fsnotify.NewWatcher()`'s error was discarded and then overwritten, so a
  failure would nil-panic on the next line.
* `watcher.Add()`'s error was never checked.
* The watcher was never closed, leaking one watcher and one goroutine per deck
  load.
* A `select` with a single case, and `fmt.Printf` rather than the logging
  helpers.
* Any filesystem event triggered a reload, and an invalid config called
  `fatal()`, killing deckmaster on a typo.

It was rewritten as `Deck.watchForChanges`: errors are checked, the watcher is
closed via `defer`, only write/create/rename events trigger a reload, the
goroutine exits once another deck becomes active, and an invalid config is
logged while the working one is kept.

## Other fixes made during integration

* `#### Recent Window (requires pulseaudio)` in #87's README section was a
  copy-paste of the X11 Recent Window heading and trailing sentence. Rewritten
  as *Microphone Mute*.
* Duplicate *Background Image* and *Re-using another deck's configuration*
  sections, an artifact of #99 relocating them while other PRs edited them in
  place, were removed.
* `verbosef` was renamed to `verboseLog` by #147; the remaining call sites from
  other PRs were updated.
* `ButtonWidget.LoadImage` gained an explicit destination parameter in #147; the
  callers in #93 and #105 were updated.
* Fixed an unbuffered `signal.Notify` channel in #147's `reapChildProcesses`
  (flagged by `go vet`).

## Verification

```
go build ./...   # ok
go vet ./...     # clean
go test ./...    # ok
gofmt -l .       # clean
```

`config_test.go` was added during integration. It checks that every bundled
`.deck` parses and only uses widget ids that `NewWidget` actually handles, that
deck inheritance and circular-parent detection work, that every `ConfigValue`
conversion behaves (including the `time.Duration` cases added by #66), and that
the config fields contributed by #133, #134 and #147 survive a TOML round trip.

There were no tests in the repository before this branch, so the suite covers
the merged configuration surface rather than the project as a whole. The widget
rendering and PulseAudio/DBus integration paths need real hardware and a running
session bus, and are not exercised.
