//go:build linux && arm

package battery

import "unsafe"

// Compile-time layout checks for 32-bit ARM: a mismatch makes one of the
// constant expressions negative and the test build fails, so
// `GOOS=linux GOARCH=arm go test -c` or `go vet` catches it without hardware.
var (
	_ [unsafe.Sizeof(i2cMsg{}) - 12]byte
	_ [12 - unsafe.Sizeof(i2cMsg{})]byte
	_ [unsafe.Sizeof(i2cRdwrIoctlData{}) - 8]byte
	_ [8 - unsafe.Sizeof(i2cRdwrIoctlData{})]byte
)
