package main

import (
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	dbusInterface   = "io.github.muesli.DeckMaster"
	dbusMonitorPath = "/Monitor"
	introInterface  = "org.freedesktop.DBus.Introspectable"
	intro           = `<node>
	<interface name="` + dbusInterface + `">
		<method name="ActiveWindowChanged">
			<arg direction="in" type="s" />
			<arg direction="in" type="s" />
		</method>
	</interface>` + introspect.IntrospectDataString + "<node>"
)

type ActiveWindow struct {
	resource string
	title    string
	id       string
}

type WindowChannel struct {
	channel chan ActiveWindow
}

// ActiveWindowChanged is exported on the session bus; the *dbus.Error return
// value is part of the signature godbus requires, even though it is never set.
func (w *WindowChannel) ActiveWindowChanged(class, title, id string) *dbus.Error { //nolint:unparam
	w.channel <- ActiveWindow{class, title, id}
	return nil
}

func MonitorActiveWindowChanged() <-chan ActiveWindow {
	w := WindowChannel{make(chan ActiveWindow)}
	cnn, e := dbus.SessionBus()
	if e != nil {
		errorLog(e, "failed to connect to the session bus, active window tracking disabled")
		return w.channel
	}
	_ = cnn.Export(&w, dbusMonitorPath, dbusInterface)

	introspectable := introspect.Introspectable(intro)
	_ = cnn.Export(introspectable, dbusMonitorPath, introInterface)

	reply, e := cnn.RequestName(dbusInterface, dbus.NameFlagDoNotQueue)
	if e != nil {
		errorLog(e, "failed to request name on active window changed")
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		errorLogF("service '%s' already running", dbusInterface)
	}
	return w.channel
}

func CallDBus(object, path, method string, args ...interface{}) error {
	cnn, err := dbus.SessionBus()
	if err != nil {
		return err
	}

	o := cnn.Object(object, dbus.ObjectPath(path))
	return o.Call(method, 0, args...).Err
}
