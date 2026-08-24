package main

import (
	"fmt"
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
)

type MediaPlayer struct {
	conn      *dbus.Conn
	eventCh   chan<- interface{}
	busName   string
	ownerName string

	name   string
	busObj dbus.BusObject
	status MediaPlayerStatus

	mutex sync.RWMutex
}

func NewMediaPlayer(conn *dbus.Conn, eventCh chan<- interface{}, busName string, ownerName string) *MediaPlayer {
	return &MediaPlayer{
		conn:      conn,
		eventCh:   eventCh,
		busName:   busName,
		ownerName: ownerName,
	}
}

func (p *MediaPlayer) Initialize() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.busObj = p.conn.Object(p.busName, "/org/mpris/MediaPlayer2")
	p.name = p.busName

	var identity string
	if err := p.busObj.Call("org.freedesktop.DBus.Properties.Get", 0, "org.mpris.MediaPlayer2", "Identity").Store(&identity); err != nil {
		errorLog(err, "error while getting the identity for player %s", p.busName)
	}
	if len(identity) != 0 {
		p.name = identity
	}

	playbackStatus, err := p.busObj.GetProperty("org.mpris.MediaPlayer2.Player.PlaybackStatus")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error while getting playback status for player %s: %s", p.name, err)
	} else {
		if variant, ok := playbackStatus.Value().(string); ok {
			p.status.UpdatePlaybackStatus(variant)
		}
	}

	metadata, err := p.busObj.GetProperty("org.mpris.MediaPlayer2.Player.Metadata")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error while getting metadata for player %s: %s", p.name, err)
	} else {
		if variant, ok := metadata.Value().(map[string]dbus.Variant); ok {
			p.status.UpdateFromMetadata(variant)
		}
	}

	p.busObj.AddMatchSignal("org.freedesktop.DBus.Properties", "PropertiesChanged")
}

func (p *MediaPlayer) Close() {
	p.busObj.RemoveMatchSignal("org.freedesktop.DBus.Properties", "PropertiesChanged")
}

// call invokes a player method and logs a failure instead of propagating it,
// as none of the player controls have a way to report back to the user.
func (p *MediaPlayer) call(method string) {
	if err := p.busObj.Call(method, 0).Store(); err != nil {
		errorLog(err, "error while calling %s for player %s", method, p.name)
	}
}

func (p *MediaPlayer) Stop() {
	p.call("org.mpris.MediaPlayer2.Player.Stop")
}

func (p *MediaPlayer) Play() {
	p.call("org.mpris.MediaPlayer2.Player.Play")
}

func (p *MediaPlayer) PlayPause() {
	p.call("org.mpris.MediaPlayer2.Player.PlayPause")
}

func (p *MediaPlayer) Previous() {
	p.call("org.mpris.MediaPlayer2.Player.Previous")
}

func (p *MediaPlayer) Next() {
	p.call("org.mpris.MediaPlayer2.Player.Next")
}

func (p *MediaPlayer) Status() MediaPlayerStatus {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return p.status
}

func (p *MediaPlayer) onPropertiesChanged(propertiesVariant map[string]dbus.Variant) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.status.UpdateFromPropertiesVariant(propertiesVariant)
	p.eventCh <- MediaPlayerStatusChanged{PlayerName: p.name, Status: p.status}
}
