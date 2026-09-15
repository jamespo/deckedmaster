# deckedmaster

[![Latest Release](https://img.shields.io/github/release/jamespo/deckedmaster.svg?style=for-the-badge)](https://github.com/jamespo/deckedmaster/releases)
[![Go Doc](https://img.shields.io/badge/godoc-reference-blue.svg?style=for-the-badge)](https://pkg.go.dev/github.com/jamespo/deckedmaster)
[![Software License](https://img.shields.io/badge/license-MIT-blue.svg?style=for-the-badge)](/LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/jamespo/deckedmaster/build.yml?branch=master&style=for-the-badge)](https://github.com/jamespo/deckedmaster/actions)
[![Go ReportCard](https://goreportcard.com/badge/github.com/jamespo/deckedmaster?style=for-the-badge)](https://goreportcard.com/report/jamespo/deckedmaster)

An application to control your Elgato Stream Deck or Ajazz AKP03E on Linux

## About this fork

**deckedmaster** is a fork of
[muesli/deckmaster](https://github.com/muesli/deckmaster/) by Christian
Muehlhaeuser, which is where all the original design and the bulk of the code
comes from. All credit for that work belongs upstream; this fork exists to keep
the project moving, add Ajazz AKP03E support, and integrate the pull requests
that were left open there.

Development in this fork is carried out with AI assistance, so please review
changes with that in mind — bug reports and pull requests are very welcome.

## Supported devices

- Elgato Stream Deck, Stream Deck MK.2, Stream Deck Mini, Stream Deck Mini MK.2, Stream Deck XL
- Ajazz AKP03E (both hardware revisions, USB PID `1002` and `3002`)

The Ajazz AKP03E's 3 rotary encoders can be bound to actions both for
pressing them (`keys.action`/`keys.action_hold`, same as any other key) and
for turning them (`keys.action_rotate_left`/`keys.action_rotate_right` — see
[Rotary encoder actions](#rotary-encoder-actions)). They have no screen, so
there's nothing to configure under `keys.widget` for them. See
[decks/ajazz_akp03e.deck](decks/ajazz_akp03e.deck) for a full example,
including using the left encoder to control the volume.

> **This branch integrates all pull requests that were open upstream.**
> Some of them overlapped and had to be reconciled — most notably, the `mute`
> widget id now refers to output muting, and microphone muting moved to
> `microphoneMute`. See [MERGED_PULL_REQUESTS.md](MERGED_PULL_REQUESTS.md) for
> the full list and every behavioural difference.

## Features

- Multiple pages & navigation between decks
- Buttons (icons & text)
- Background images, including per-key background colors
- Brightness control
- Supports different actions for short & long presses
- Automatically reloads a deck when its config file changes
- Per-window key overrides (X11 or KWin)
- Comes with a collection of widgets:
    - Buttons
    - Time (with formatting)
    - Timer / countdown
    - CPU/Mem usage
    - Hardware temperature sensor data
    - Weather
    - Command output (text or image)
    - Recently used windows (X11-only)
    - Audio output/input selection (PulseAudio)
    - Mute/unmute speakers or microphone (PulseAudio)
    - Per-application volume control (PulseAudio)
    - Media player controls (MPRIS/DBus)
    - Spotify
    - niri compositor state (Linux-only)
    - umbriel compositor state (Linux-only)
- Lets you trigger several actions:
    - Run commands
    - Emulate a key-press
    - Paste to clipboard
    - Trigger a dbus call
    - Control media players
    - Drive the niri compositor (Linux-only)
    - Drive the umbriel compositor (Linux-only)
- REST based API:
    - Read and update device state like brightness and sleep/fade time.
    - Wake or put the device to sleep.
    - Read and modify the active deck config or an individual key configuration.
    - Read widget state.
    - Interactive API documentation using Swagger

## Installation

### Packages

- [Packages](https://github.com/jamespo/deckedmaster/releases) in Alpine, Debian & RPM formats
- [Binaries](https://github.com/jamespo/deckedmaster/releases) for various architectures

### From source

Make sure you have a working Go environment (Go 1.17 or higher is required).
See the [install instructions](https://golang.org/doc/install.html).

To install deckedmaster, simply run:

    git clone https://github.com/jamespo/deckedmaster.git
    cd deckedmaster
    go build

## System Setup

On Linux you need to set up some `udev` rules to be able to access the device as
a regular user. Edit `/etc/udev/rules.d/60-streamdeck.rules` and add these lines:

```
SUBSYSTEM=="usb", ATTRS{idVendor}=="0fd9", ATTRS{idProduct}=="0060", TAG+="uaccess", SYMLINK+="streamdeck"
SUBSYSTEM=="usb", ATTRS{idVendor}=="0fd9", ATTRS{idProduct}=="006d", TAG+="uaccess", SYMLINK+="streamdeck"
SUBSYSTEM=="usb", ATTRS{idVendor}=="0fd9", ATTRS{idProduct}=="0080", TAG+="uaccess", SYMLINK+="streamdeck"
SUBSYSTEM=="usb", ATTRS{idVendor}=="0fd9", ATTRS{idProduct}=="0063", TAG+="uaccess", SYMLINK+="streamdeck-mini"
SUBSYSTEM=="usb", ATTRS{idVendor}=="0fd9", ATTRS{idProduct}=="0090", TAG+="uaccess", SYMLINK+="streamdeck-mini"
SUBSYSTEM=="usb", ATTRS{idVendor}=="0fd9", ATTRS{idProduct}=="006c", TAG+="uaccess", SYMLINK+="streamdeck-xl"
SUBSYSTEM=="usb", ATTRS{idVendor}=="0300", ATTRS{idProduct}=="1002", TAG+="uaccess", SYMLINK+="streamdeck-akp03e"
SUBSYSTEM=="usb", ATTRS{idVendor}=="0300", ATTRS{idProduct}=="3002", TAG+="uaccess", SYMLINK+="streamdeck-akp03e"
```

Reload the rules with `sudo udevadm control --reload-rules`. Unplug and re-plug
the device, and you should be good to go.

> **The `60-` filename prefix matters.** systemd applies the `uaccess` tag from
> `/usr/lib/udev/rules.d/73-seat-late.rules`, and `udev` evaluates rule files in
> filename order. If you put these rules in a file that sorts *after* 73 — such
> as the `99-streamdeck.rules` that older versions of this README (and most
> Stream Deck guides) suggest — then `TAG+="uaccess"` is applied too late, the
> `uaccess` builtin has already run, and no ACL is ever granted. Any prefix
> below 73 works; `60-` is a safe choice.

### Troubleshooting: `failed to initialize Stream Deck: hidapi: failed to open device`

This is almost always the udev permissions above not taking effect. deckedmaster
talks to the device through hidapi's **libusb** backend, so it opens
`/dev/bus/usb/BBB/DDD` — *not* `/dev/hidraw*`. Adding `SUBSYSTEM=="hidraw"`
rules will not help on its own.

Find your device and check whether you actually have access to its node:

```console
$ lsusb | grep -i ajazz
Bus 003 Device 002: ID 0300:3002 Ajazz HOTSPOTEKUSB HID DEMO
$ getfacl /dev/bus/usb/003/002 | grep "^user:"
user:james:rw-
```

If that `user:` line for your username is missing, the `uaccess` ACL was not
applied. In order of likelihood:

1. **Your rules file sorts after `73-seat-late.rules`** — see the note above;
   rename it to `60-streamdeck.rules` and reload.
2. **The rules were never reloaded or the device was never re-plugged.** Run
   `sudo udevadm control --reload-rules`, then either re-plug or
   `sudo udevadm trigger --action=add --subsystem-match=usb`.
3. **You have no active local seat session.** `uaccess` only grants the ACL to a
   user logged in on a local seat; `loginctl` should show a session for you with
   a `SEAT` of `seat0`. Over plain SSH it will not, and you need a group-based
   rule instead: add `MODE="0660", GROUP="plugdev"` to the rule lines and make
   sure you're in that group (`sudo usermod -aG plugdev $USER`, then log out and
   back in). Substitute any group you belong to if your distro has no `plugdev`.

To confirm it really is permissions rather than a device problem, running
deckedmaster once with `sudo` should get past this error. Don't leave it that way
— fix the rules instead.

### Starting deckedmaster automatically

If you want deckedmaster to be started automatically upon device plugin, you can
use systemd path activation, adding `streamdeck.path` and `streamdeck.service`
files to `$HOME/.config/systemd/user`.

`streamdeck.path` contents:

```ini
[Unit]
Description="Stream Deck Device Path"

[Path]
# the device name will be different if you use streamdeck-mini or streamdeck-xl
PathExists=/dev/streamdeck
Unit=streamdeck.service

[Install]
WantedBy=multi-user.target
```

`streamdeck.service` contents:

```ini
[Unit]
Description=Deckedmaster Service

[Service]
# adjust the path to deckedmaster and .deck file to suit your needs
ExecStart=/usr/local/bin/deckedmaster --deck path-to/some.deck
Restart=on-failure
ExecReload=kill -HUP $MAINPID

[Install]
WantedBy=default.target
```

Then enable and start the `streamdeck.path` unit:

```bash
systemctl --user enable streamdeck.path
systemctl --user start streamdeck.path
```

### Wayland / KWin

Window tracking (the `recentWindow` widget and
[per-window key overrides](#per-window-key-overrides)) uses X11 by default. On
Wayland, X11 window tracking is unavailable, so deckedmaster instead exposes a
DBus service at `io.github.jamespo.DeckedMaster` and waits for the compositor to
tell it which window is active.

For KWin (KDE Plasma), a small script that sends those notifications ships in
the `kwin/` directory. Install and enable it with:

```bash
kpackagetool6 --type=KWin/Script --install kwin
kwriteconfig6 --file kwinrc --group Plugins --key deckedmasterEnabled true
qdbus6 org.kde.KWin /KWin reconfigure
```

On Plasma 5, use `kpackagetool5`, `kwriteconfig5` and `qdbus` instead. You can
also enable the script from *System Settings → Window Management → KWin
Scripts* once it is installed.

Other compositors can drive the same interface by calling the
`ActiveWindowChanged` method on `/Monitor` with the window's resource name,
title and id.

### Wayland / niri

[niri](https://github.com/YaLTeR/niri) is supported natively, over its own IPC
socket: see the [niri widget](#niri-linux-only) for the state a key can show
and [niri actions](#niri-compositor-actions-linux-only) for what a key can do.
This is separate from the window tracking above - it needs no DBus service and
no compositor-side script, only `$NIRI_SOCKET`, which niri exports into every
process it starts.

If you run deckedmaster from a systemd user unit rather than from niri itself,
add `ExecStartPre=/bin/sleep 5` to its `[Service]` section: started too early,
it comes up before niri has finished setting up. The unit also needs
`$NIRI_SOCKET` in its environment, which `niri-session` arranges by importing
niri's environment into the user session - check with
`systemctl --user show-environment | grep NIRI_SOCKET` if keys stay blank.

### Wayland / umbriel

[umbriel](https://docs.noctalia.dev/umbriel/) is supported natively, through
its own client: see the [umbriel widget](#umbriel-linux-only) for the state a
key can show and [umbriel actions](#umbriel-compositor-actions-linux-only) for
what a key can do. Like the niri support above, this needs no DBus service and
no compositor-side script.

Where niri has to be asked for its state, umbriel offers it. `umbriel
subscribe` streams one JSON object per line, and each event is a full snapshot
- sent as soon as the stream opens, and again whenever the thing it describes
changes. deckedmaster keeps one such subscription open for as long as it runs
and holds the latest snapshot in memory, so repainting a key reads a value that
is already there and performs no IPC at all. A key's `interval` therefore
decides how often it is repainted, not how often the compositor is asked
anything.

If the compositor goes away - a restart, or a session that has not come up yet
- the subscription is reopened, backing off up to half a minute between
attempts, and keys draw their placeholder until a fresh snapshot arrives rather
than showing state left behind by a compositor that has since exited.

The `umbriel` binary has to be on `$PATH`. It finds the compositor the way
every other umbriel client does - `$UMBRIEL_SOCKET` when it is set, otherwise
derived from `$WAYLAND_DISPLAY` - so there is no socket path to configure here.
Running deckedmaster from a systemd user unit needs one of those two in the
unit's environment; check with `systemctl --user show-environment | grep -E
'UMBRIEL_SOCKET|WAYLAND_DISPLAY'` if keys stay blank.

## Usage

Start `deckedmaster` with the initial deck configuration you want to load:

```bash
deckedmaster -deck deck/main.deck
```

You can control the brightness, in percent:

```bash
deckedmaster -brightness 50
```

Control a specific streamdeck:

```bash
deckedmaster -device [serial number]
```

Set a sleep timeout after which the screen gets turned off:

```bash
deckedmaster -sleep 10m
```

```bash
deckedmaster -rest-listen [localhost:4321] [-rest-trusted-proxies [10.0.0.1,10.0.0.2]] [-rest-docs]
```

Set an interface/ip and/or port to start a REST server on.
No authentication is required so it is **STRONGLY** recommended to only use *localhost*, else your device may be accessible over the internet.
If you do intend to expose the API to anything other than the local interface, use a reverse proxy to handle authentication and set the `-rest-trusted-proxies` flag.
If the `X-Forwarded-For` request header is set its value will only replace the client IP if the connection IP is in the optional `-rest-trusted-proxies` list, otherwise any client could spoof their IP with this header. It is not needed if you are not using a reverse proxy.
The interactive Swagger documentation will be available as `/docs` if `-rest-docs` is also set.

## Configuration

You can find a few example configurations in the [decks](https://github.com/jamespo/deckedmaster/tree/master/decks)
directory. Edit them to your needs!

### Background Image

You can configure each deck to display an individual wallpaper behind its
widgets:

```toml
background = "/some/image.png"
```

A key can override the deck background with a solid color:

```toml
[[keys]]
  index = 1
  backgroundColor = "#ff0000" # Sets the background to solid red.
```

The color is by default rendered on top of any deck background image. If that
image has transparency, you can also specify `backgroundMode = "blend"` to have
the background color show through only the transparent image regions. This solid
or blended background is static; the widget is then rendered on top of it.

### Re-using another deck's configuration

If you specify a `parent` inside a deck's configuration, it will inherit all
of the parent's settings that are not overwritten by the deck's own settings.
This even works recursively:

```toml
parent = "another.deck"
```

### Per-window key overrides

A deck can override individual keys depending on which window currently has
focus. Add one or more `[[window]]` blocks; `resource` and `title` are regular
expressions matched against the active window, and the keys inside the block
replace the deck's own keys for as long as that window is focused:

```toml
[[window]]
  resource = "firefox"
  title = ".*YouTube.*"

  [[window.keys]]
    index = 0
    [window.keys.widget]
      id = "mediaPlayer"
```

When no `[[window]]` block matches, the deck's regular keys are restored.
Window tracking needs either X11 or the bundled KWin script (see
[Wayland / KWin](#wayland--kwin)).

### Automatic configuration reloading

deckedmaster watches the active deck file and reloads it automatically when it
changes on disk, so you can edit a `.deck` file and see the result immediately.
If the edited configuration is invalid, the previous one is kept and the error
is printed to stderr.

You can still trigger a reload explicitly by sending `SIGHUP`:

```bash
kill -HUP $(pidof deckedmaster)
```

### Widgets

Any widget is build up the following way:

```toml
[[keys]]
  index = 0
  name = "My key" # optional
```

`index` needs to be present in every widget and describes the position of the
widget on the streamdeck. `index` is 0-indexed and counted from top to bottom
and left to right.
`name` is an optional string which can be used instead of the index to identify
keys/widgets in the REST API. Must be unique inside the active deck but may be
reused across different decks.

#### Update interval for widgets

Optionally, you can configure an update `interval` for each widget:

```toml
[keys.widget]
  id = "button"
  interval = 500 # optional
```

The attribute `interval` defines the time in `ms` between two consecutive
updates of a widget.

#### Button

A simple button that can display an image and/or a label.

```toml
[keys.widget]
  id = "button"
  [keys.widget.config]
    icon = "/some/image.png" # optional
    label = "My Button" # optional
    fontsize = 10.0 # optional
    color = "#fefefe" # optional
    flatten = true # optional
```

If `flatten` is `true` all opaque pixels of the icon will have the color `color`.

#### Recent Window (requires X11)

Displays the icon of a recently used window/application. Pressing the button
activates the window.

```toml
[keys.widget]
  id = "recentWindow"
  [keys.widget.config]
    window = 1
    showTitle = true # optional
```

If `showTitle` is `true`, the title of the window will be displayed below the
window icon.

#### Microphone Mute (requires PulseAudio)

Displays the mute status of the default PulseAudio *source* (your microphone).
Pressing the button toggles the state.

```toml
[keys.widget]
  id = "microphoneMute"
  [keys.widget.config]
    icon = "assets/microphone-rec.png"
    iconMute = "assets/microphone-mute.png"
```

`icon` is shown while the microphone is live, `iconMute` while it is muted.

> **Note:** this widget's id used to be `mute`. That id now belongs to the
> [Mute](#mute-requires-pulseaudio) widget, which mutes audio *output*. See
> [Audio widgets](#audio-widgets) for the difference.

#### Audio (requires PulseAudio)

Displays the currently selected PulseAudio sink (output) or source (input) and
lets you cycle between the available devices.

```toml
[keys.widget]
  id = "audio"
  [keys.widget.config]
    stream = "sink" # or "source"
```

#### Mute (requires PulseAudio)

Displays and toggles the mute state of the default PulseAudio *sink* (your
speakers/headphones).

```toml
[keys.widget]
  id = "mute"
  [keys.widget.config]
    stream = "sink" # or "source"
    muted = false # optional, the icon state to start from
```

##### Audio widgets

deckedmaster ships three PulseAudio widgets that overlap slightly:

| Widget | Controls | Comes from |
| --- | --- | --- |
| `mute` | Mute state of a sink or source | PR #147 |
| `audio` | Which sink/source is selected | PR #147 |
| `microphoneMute` | Mute state of the default source, with custom icons | PR #87 |
| `pulseAudioControl` | Per-application playback streams | PR #93 |

#### Time

A flexible widget that can display the current time or date.

Two presets are available: `clock` (equivalent to `format = "%H;%i;%s"` with
`font = "bold;regular;thin"`) and `date` (equivalent to `format = "%l;%d;%M"`
with `font = "regular;bold;regular"`). Both ignore any further `config`.

```toml
[keys.widget]
  id = "time"
  [keys.widget.config]
    format = "%H;%i;%s"
    font = "bold;regular;thin" # optional
    color = "#fefefe" # optional
    layout = "0x0+72x24;0x24+72x24;0x48+72x24" # optional
```

With `layout` custom layouts can be definded in the format `[posX]x[posY]+[width]x[height]`.

Values for `format` are:

| %   | gets replaced with                                                 |
| --- | ------------------------------------------------------------------ |
| %Y  | A full numeric representation of a year, 4 digits                  |
| %y  | A two digit representation of a year                               |
| %F  | A full textual representation of a month, such as January or March |
| %M  | A short textual representation of a month, three letters           |
| %m  | Numeric representation of a month, with leading zeros              |
| %l  | A full textual representation of the day of the week               |
| %D  | A textual representation of a day, three letters                   |
| %d  | Day of the month, 2 digits with leading zeros                      |
| %h  | 12-hour format of an hour with leading zeros                       |
| %H  | 24-hour format of an hour with leading zeros                       |
| %i  | Minutes with leading zeros                                         |
| %s  | Seconds with leading zeros                                         |
| %a  | Lowercase Ante meridiem and Post meridiem                          |
| %t  | Timezone abbreviation                                              |

#### Top

This widget shows the current CPU or memory utilization as a bar graph.

```toml
[keys.widget]
  id = "top"
  [keys.widget.config]
    mode = "cpu"
    color = "#fefefe" # optional
    fillColor = "#d497de" # optional
    borderColor = "#ffffff" # optional
```

There are two values for `mode`: `cpu` and `memory`.

#### Command

A widget that displays the output of commands.

```toml
[keys.widget]
  id = "command"
  [keys.widget.config]
    command = "echo 'Files:'; ls -a ~ | wc -l"
    font = "regular;bold" # optional
    color = "#fefefe" # optional
    layout = "0x0+72x20;0x20+72x52" # optional
```

#### Image Command

Like `command`, but the command is expected to print the *path of an image
file*, which is then rendered on the key. Useful for icons that change based on
external state.

```toml
[keys.widget]
  id = "imageCommand"
  interval = 1000 # optional
  [keys.widget.config]
    command = "~/bin/current-status-icon.sh"
```

The command is re-run on every update `interval` (defaults to one second).

#### Weather

A widget that displays the weather condition and temperature.

```toml
[keys.widget]
  id = "weather"
  [keys.widget.config]
    location = "MyCity" # optional
    unit = "celsius" # optional
    color = "#fefefe" # optional
    flatten = true # optional
    theme = "openmoji" # optional
```

The supported location types can be found [here](http://wttr.in/:help). The unit
has to be either `celsius` or `fahrenheit`. If `flatten` is `true` all opaque
pixels of the condition icon will have the color `color`. In case `theme` is set
corresponding icons with correct names need to be placed in
`~/.local/share/deckedmaster/themes/[theme]`. The default icons with their
respective names can be found [here](https://github.com/jamespo/deckedmaster/tree/master/assets/weather).

#### Timer

A flexible widget that can display a timer/countdown.

```toml
[keys.widget]
  id = "timer"
  [keys.widget.config]
    times = "5s;10m;30m;1h5m" # optional
    font = "bold;regular;thin" # optional
    color = "#fefefe;#0f0f0f;#00ff00;" # optional
    adaptive = "false" # optional
    underflow = "false" # optional
    underflowColor = "#ff0000;#ff0000;#ff0000" # optional
```

With `layout` custom layouts can be definded in the format `[posX]x[posY]+[width]x[height]`.

Values for `format` are:

| %   | gets replaced with                                                 |
| --- | ------------------------------------------------------------------ |
| %h  | 12-hour format of an hour with leading zeros                       |
| %H  | 24-hour format of an hour with leading zeros                       |
| %i  | Minutes with leading zeros                                         |
| %I  | Minutes without leading zeros                                      |
| %s  | Seconds with leading zeros                                         |
| %S  | Seconds without leading zeros                                      |

The timer can be started and paused by short pressing the button.
When triggering the hold action the next timer in the times list is selected if
no timer is running. If the timer is paused, it will be reset.
The setting underflow determines whether the timer keeps ticking after exceeding its deadline.
The adaptive settings allows the removal of leading zeroes and delimiters that are not required for the time representation.

#### Temperature

This widget shows hardware temperature sensor data as a bar graph.

```toml
[keys.widget]
  id = "temp"
  [keys.widget.config]
    sensorKey = "k10temp_tctl"
    label = "CPU"
    maxTemp = 100 #optional
    color = "#fefefe" # optional
    fillColor = "#d497de" # optional
```

The values for sensorKey are platform dependent. On most Linux systems they're
based on the [hwmon-sysfs-interface](https://www.kernel.org/doc/Documentation/hwmon/sysfs-interface) under `/sys/class/hwmon`.

Example: `k10temp_tctl`

- `k10temp`: the name for a specific sensor interface, for example, the value
reported from `/sys/class/hwmon/hwmon0/name`
- `tctl`: the label for a specific sensor, for example, the value reported from
`/sys/class/hwmon/hwmon0/temp1_label` (label is converted to lowercase and
spaces are replaced with underscores)
- For this example, the temperature value would be available in `/sys/class/hwmon/hwmon0/temp1_input`

For the full explanation on how these sensor keys are created on all platforms,
check the `SensorTemperatures*` functions in the [gopsutil](https://github.com/shirou/gopsutil/blob/master/host/host.go)
package.

#### Spotify

A widget that displays the current album and track position.

```toml
[keys.widget]
  id = "spotify"
  interval = 500
  [keys.widget.config]
    fontsize = 8
    color = "#ffffff" 
   
```

This widget depends on the [spotifycli](https://github.com/pwittchen/spotify-cli-linux) application for Linux.

The widget will display the album along with the track position of the current song. The album art is loaded directly 
from the url provided by the spotify via the spotifycli cli.  

#### Pulseaudio Control

A widget that can controls a specific pulseaudio input sink (like Rhythmbox, Firefox etc.). 

```toml
[keys.widget]
  id = "pulseAudioControl"
  [keys.widget.config]
    appName = "Application name" # Like "Rhythmbox"
    mode = "mute" # Only mute is supported at the moment.
    showTitle = true # optional
```

This widget is using "pactl" and "pacmd".

You can use `pacmd list-sink-inputs` to see the current sinks. Use the value of "application.name" for appName.

#### Media player

A widget that displays media player information.

```toml
[keys.widget]
  id = "mediaPlayer"
  [keys.widget.config]
    mode = "art" # optional
    player_name = true # optional
    icon_playing = "images/stop.jpg"
    icon_paused = "images/play.jpg"
    icon_no_player = "images/no-player.jpg"
```

The supported modes are `art` (default) and `playback status`. If `playback 
status` has been chosen, the icons `icon_playing`, `icon_paused` or 
`icon_no_player` will be shown depending on the playback status of the selected
media player.

#### Niri (Linux only)

A widget that displays the state of the [niri](https://github.com/YaLTeR/niri)
Wayland compositor, one field per line:

```toml
[keys.widget]
  id = "niri"
  interval = 1000 # optional
  [keys.widget.config]
    fields = ["ws-name", "ws-idx", "ws-count"]
    font = ["regular", "bold", "thin"] # optional
    color = ["#8899aa", "#ffffff", "#8899aa"] # optional
    layout = "0x0+72x20;0x20+72x52" # optional
    width = 10 # optional
    placeholder = "-" # optional
```

The fields a key can draw are:

| Field | Shows |
| --- | --- |
| `ws-name` | focused workspace name, or `ws` if it has none |
| `ws-idx` | focused workspace index |
| `ws-count` | `<idx>/<total>` for the focused output |
| `win-app` | focused window's app id, last component only |
| `win-title` | focused window's title |
| `win-pos` | `<column>/<columns>` in the focused workspace |
| `output` | focused output name |
| `output-count` | `<n> mon` |
| `overview` | `OPEN` or `closed` |

Anything else in `fields` is drawn literally, which is how a key gets a static
caption above a live value - `fields = ["Monitor", "output", "output-count"]`.

`width` is the number of characters before a window title or workspace name is
truncated; key screens are around 60 pixels wide, so a title that is not cut
down renders as a smudge. `placeholder` is drawn in place of state niri does
not have, e.g. the title of a window when nothing is focused.

deckedmaster talks to niri over its IPC socket directly, so no `niri msg`
process is forked to paint a key. Replies are cached for a second in
`$XDG_RUNTIME_DIR/niri-deck-<uid>/`, one file per query, named after the
`niri msg` subcommand that returns the same JSON - so six keys refreshing on
the same tick cost one round trip each rather than one per key, and what a key
is showing can be looked at from a shell. Performing a niri action drops the
cache, so a key reflects a press on its very next refresh.

See [decks/niri.deck](decks/niri.deck) for a complete deck built out of this
widget and the actions below.

#### Umbriel (Linux only)

A widget that displays the state of the
[umbriel](https://docs.noctalia.dev/umbriel/) Wayland compositor, one field per
line:

```toml
[keys.widget]
  id = "umbriel"
  interval = 1000 # optional
  [keys.widget.config]
    fields = ["ws-name", "ws-idx", "ws-count"]
    font = ["regular", "bold", "thin"] # optional
    color = ["#8899aa", "#ffffff", "#8899aa"] # optional
    layout = "0x0+72x20;0x20+72x52" # optional
    width = 10 # optional
    placeholder = "-" # optional
```

The fields a key can draw are:

| Field | Shows |
| --- | --- |
| `ws-name` | focused workspace name, or `ws` if it has none |
| `ws-idx` | focused workspace index |
| `ws-count` | `<idx>/<total>` for the focused output |
| `ws-layout` | the workspace's layout mode: `scrolling`, `dwindle` or `master` |
| `win-app` | focused window's app id, last component only |
| `win-title` | focused window's title |
| `win-pos` | `<column>/<columns>` in the focused workspace |
| `win-count` | `<n> win` on the focused workspace |
| `win-float` | `float` or `tile` for the focused window |
| `output` | focused output name |
| `output-count` | `<n> mon` |
| `overview` | `OPEN` or `closed` |
| `submap` | the active keybind submap, or nothing in the default context |
| `kb-layout` | the active keyboard layout |
| `scratchpad` | how many windows are stashed in a scratchpad |

Anything else in `fields` is drawn literally, which is how a key gets a static
caption above a live value - `fields = ["Monitor", "output", "output-count"]`.

`width` is the number of characters before a window title or workspace name is
truncated; key screens are around 60 pixels wide, so a title that is not cut
down renders as a smudge. `placeholder` is drawn in place of state umbriel does
not have, e.g. the title of a window when nothing is focused, or `submap` while
the session is in its default context.

A note on which window these fields mean: umbriel marks one window per occupied
workspace as focused - the one that would take the focus back if you switched
to it - and exactly one window in the whole session as active. The `win-*`
fields draw the active one, which is the window you are actually typing into.

Every value comes off the event stream described under
[Wayland / umbriel](#wayland--umbriel), so no process is forked to paint a key.

See [decks/umbriel.deck](decks/umbriel.deck) for a complete deck built out of
this widget and the actions below.

### Actions

You can hook up any key with several actions. A regular keypress will trigger
the widget's configured `keys.action`, while holding the key will trigger
`keys.action_hold`. Any of the actions below can be used in any of these
slots.

#### Rotary encoder actions

Devices with rotary encoders/dials (currently just the Ajazz AKP03E) let you
bind separate actions to turning them, independent of pressing them:

```toml
[keys.action_rotate_left]
  device = "volume-5"
[keys.action_rotate_right]
  device = "volume+5"
```

`keys.action_rotate_left` fires once per detent turned counter-clockwise,
`keys.action_rotate_right` once per detent turned clockwise. Since the
encoders have no screen, a key bound this way usually has no `keys.widget`
at all - see [decks/ajazz_akp03e.deck](decks/ajazz_akp03e.deck).

#### Switch deck

```toml
[keys.action]
  deck = "relative/path/to/another.deck"
```

#### Run a command

```toml
[keys.action]
  exec = "some_command --with-parameters"
```

The command is not run through a shell, but a leading `~` in the command or in
any of its parameters is expanded to your home directory:

```toml
[keys.action]
  exec = "~/.config/deckedmaster/decks/sonos/bin/sonos-deck favourite-scroll +1"
```

#### Emulate key-presses

```toml
[keys.action]
  keycode = "Leftctrl-C"
```

Emulate a series of key-presses with delay in between:

```toml
[keys.action]
  keycode = "Leftctrl-X+500 / Leftctrl-V / Num1"
```

A list of available `keycodes` can be found here: [keycodes](https://github.com/jamespo/deckedmaster/blob/master/keycodes.go)

#### Paste to clipboard

```toml
[keys.action]
  paste = "some text"
```

#### Trigger a dbus call

```toml
[keys.action.dbus]
  object = "object"
  path = "path"
  method = "method"
  value = "value"
```

#### Device actions

Increase the brightness. If no value is specified, it will be increased by 10%:

```toml
[keys.action]
  device = "brightness+5"
```

Decrease the brightness. If no value is specified, it will be decreased by 10%:

```toml
[keys.action]
  device = "brightness-5"
```

Set the brightness to a specific value between 0 and 100:

```toml
[keys.action]
  device = "brightness=50"
```

Put the device into sleep mode, blanking the screen until the next key gets
pressed:

```toml
[keys.action]
  device = "sleep"
```

Increase/decrease/set the default PulseAudio sink's volume (percent, same
`+`/`-`/`=` syntax as `brightness`, requires PulseAudio). If no value is
specified for `+`/`-`, it changes by 5%:

```toml
[keys.action]
  device = "volume+5"   # or "volume-5", or "volume=50"
```

#### Media player actions

Control playback:

```toml
[keys.action]
  media_player = "play_pause"
```

Supported playback actions are `play`, `play_pause`, `stop`, `previous` and 
`next`.

Cycle through players:

```toml
[keys.action]
  media_player = "select+"
```

Use `select+` to select the next player to be active and `select-` to select
the previous player.

#### Niri compositor actions (Linux only)

Perform an action in the [niri](https://github.com/YaLTeR/niri) Wayland
compositor, over its IPC socket rather than by running `niri msg`:

```toml
[keys.action]
  niri = "maximize-column"
```

Any niri action is accepted, spelled the way `niri msg action` spells it.
Actions that take no arguments - which is nearly all of them, and all the ones
a key normally binds - need nothing more than their name.

On top of those, these commands do what a single niri action cannot:

| Command | Does |
| --- | --- |
| `ws-cycle` | next workspace on this output, wrapping past the last back to the first |
| `ws-scroll -1` / `+1` | one workspace up/down, for a dial |
| `win-scroll -1` / `+1` | one column left/right, for a dial |
| `mon-scroll -1` / `+1` | previous/next monitor, a no-op with a single output |
| `mon-next` | next monitor, a no-op with a single output |

`ws-cycle` exists because niri's own `focus-workspace-down` stops dead at the
end of the list, which is wrong for a key whose whole job is to cycle. The
monitor commands are silent no-ops on a single-output machine rather than
making niri log a warning for every detent of a dial that cannot go anywhere.

`focus-workspace` takes an index or a name:

```toml
[keys.action]
  niri = "focus-workspace dev"
```

For any other action that takes arguments, pass the JSON for the matching
variant of niri's own `Action` enum:

```toml
[keys.action]
  niri = '{"CloseWindow":{"id":42}}'
```

#### Umbriel compositor actions (Linux only)

Perform an action in the [umbriel](https://docs.noctalia.dev/umbriel/) Wayland
compositor:

```toml
[keys.action]
  umbriel = "window-toggle-maximize"
```

Any umbriel action is accepted, spelled the way `umbriel msg` spells it, with
any argument after a colon - `workspace-switch:3`, `window-set-width:0.5`,
`spawn:foot`. Run `umbriel msg --help` for the full list.

On top of those, these commands do what a single umbriel action cannot:

| Command | Does |
| --- | --- |
| `ws-cycle` | next workspace on this output, wrapping past the last back to the first |
| `ws-scroll -1` / `+1` | one workspace back/forward, for a dial |
| `win-scroll -1` / `+1` | one window back/forward in layout order, for a dial |
| `mon-scroll -1` / `+1` | previous/next output, a no-op with a single output |
| `mon-next` | next output, a no-op with a single output |

`ws-cycle` exists because umbriel's own `workspace-next` stops at the end of
the list, which is wrong for a key whose whole job is to cycle. The monitor
commands do nothing at all on a single-output machine, rather than spending a
process per detent of a dial that cannot go anywhere.

Actions are sent with `umbriel msg`, one short-lived process per key press.
Nothing is forked to draw a key - the state the keys show arrives on the event
stream instead, as described under [Wayland / umbriel](#wayland--umbriel).

## Windows Support

MS Windows support is being tested, you need to install [Roboto fonts](https://fonts.google.com/specimen/Roboto) (bold, thin
& regular variants at a minimum).

To build on windows, you'll need to install MSYS2 & mingw-w64 GCC as the build requires CGO - the build will still complete
without these, but no decks will be detected.

```
export CGO_ENABLED=1
export CC=/c/msys64/mingw64/bin/gcc.exe
export PATH="/c/msys64/mingw64/bin:$PATH"
go build .
```

## More Decks!

* [deckmaster-emojis](https://github.com/muesli/deckmaster-emojis), an Emoji keyboard deck
* [deckmaster-helldivers2](https://github.com/boj/deckmaster-helldivers2), a deck for calling down Helldivers 2 game stratagems
* [deckedmaster-decks](https://github.com/jamespo/deckedmaster-decks), some ajazz-specific decks for sonos & niri control
  
## Feedback

Got some feedback or suggestions? Please
[open an issue](https://github.com/jamespo/deckedmaster/issues).

Feedback about the original project belongs upstream at
[muesli/deckmaster](https://github.com/muesli/deckmaster/issues).
