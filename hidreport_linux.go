//go:build linux

package main

// hidOutputReport builds a HID output report buffer for payload, padded to
// packetSize. Linux's hidraw write() expects a leading report-number byte
// even for devices (like the AKP03E) that don't use numbered reports, per
// the kernel's documented hidraw convention; the kernel strips it before
// putting the report on the wire.
func hidOutputReport(payload []byte, packetSize int) []byte {
	buf := make([]byte, 1+packetSize)
	copy(buf[1:], payload)
	return buf
}
