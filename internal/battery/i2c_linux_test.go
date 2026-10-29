//go:build linux

package battery

import (
	"runtime"
	"testing"
	"unsafe"
)

var _ Bus = (*LinuxBus)(nil)

func TestKernelStructSizes(t *testing.T) {
	var want struct{ msg, data, bufOffset uintptr }
	switch runtime.GOARCH {
	case "arm":
		want.msg, want.data, want.bufOffset = 12, 8, 8
	case "arm64", "amd64":
		want.msg, want.data, want.bufOffset = 16, 16, 8
	default:
		t.Skipf("no expected layout for GOARCH=%s", runtime.GOARCH)
	}

	var m i2cMsg
	if got := unsafe.Sizeof(m); got != want.msg {
		t.Errorf("sizeof(i2c_msg) = %d, want %d", got, want.msg)
	}
	if got := unsafe.Offsetof(m.buf); got != want.bufOffset {
		t.Errorf("offsetof(i2c_msg.buf) = %d, want %d", got, want.bufOffset)
	}
	if got := unsafe.Sizeof(i2cRdwrIoctlData{}); got != want.data {
		t.Errorf("sizeof(i2c_rdwr_ioctl_data) = %d, want %d", got, want.data)
	}
}
