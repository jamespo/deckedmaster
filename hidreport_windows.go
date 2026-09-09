//go:build windows

package main

// hidOutputReport builds a HID output report buffer for payload, padded to
// packetSize. Unlike Linux's hidraw, Windows' HID class driver does not
// strip a leading report-number byte from interrupt OUT writes for the
// AKP03E's report-ID-less output report: it goes out on the wire verbatim,
// which shifts every command byte-for-byte and makes the device silently
// ignore it. The real fix is not sending that leading byte in the first
// place, verified against a USB capture of the vendor's own Windows
// software, whose interrupt OUT packets start directly with "CRT" at
// offset 0. hidapi's hid_write() still zero-pads up to the device's
// declared output report length on our behalf, so this only needs to omit
// the byte, not manage padding itself.
func hidOutputReport(payload []byte, packetSize int) []byte {
	buf := make([]byte, packetSize)
	copy(buf, payload)
	return buf
}
