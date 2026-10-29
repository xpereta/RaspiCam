//go:build linux

package battery

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	ioctlI2CRdwr = 0x0707 // I2C_RDWR
	i2cMRd       = 0x0001 // I2C_M_RD
)

// i2cMsg mirrors the kernel's struct i2c_msg:
//
//	struct i2c_msg { __u16 addr; __u16 flags; __u16 len; __u8 *buf; };
//
// Go inserts the same padding before buf as the C compiler (2 bytes on
// 32-bit ARM, 6 on 64-bit targets).
type i2cMsg struct {
	addr  uint16
	flags uint16
	len   uint16
	buf   *byte
}

// i2cRdwrIoctlData mirrors the kernel's struct i2c_rdwr_ioctl_data:
//
//	struct i2c_rdwr_ioctl_data { struct i2c_msg *msgs; __u32 nmsgs; };
type i2cRdwrIoctlData struct {
	msgs  *i2cMsg
	nmsgs uint32
}

// LinuxBus is an I2C bus backed by /dev/i2c-N. The device stays open for the
// lifetime of the bus. The ioctl arguments live in this heap-allocated
// struct so no allocation happens per read.
type LinuxBus struct {
	mu   sync.Mutex
	fd   int
	reg  [1]byte
	msgs [2]i2cMsg
	data i2cRdwrIoctlData
}

// OpenBus opens the I2C character device at path (for example /dev/i2c-1).
func OpenBus(path string) (*LinuxBus, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open i2c bus %s: %w", path, err)
	}
	return &LinuxBus{fd: fd}, nil
}

// ReadRegs writes reg to the device at addr and reads len(buf) bytes in a
// single combined transaction (repeated start) using I2C_RDWR.
func (b *LinuxBus) ReadRegs(addr uint16, reg byte, buf []byte) error {
	if len(buf) == 0 || len(buf) > math.MaxUint16 {
		return fmt.Errorf("i2c read 0x%02x reg 0x%02x: invalid length %d", addr, reg, len(buf))
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fd < 0 {
		return fmt.Errorf("i2c read 0x%02x reg 0x%02x: bus closed", addr, reg)
	}

	b.reg[0] = reg
	b.msgs[0] = i2cMsg{addr: addr, flags: 0, len: 1, buf: &b.reg[0]}
	b.msgs[1] = i2cMsg{addr: addr, flags: i2cMRd, len: uint16(len(buf)), buf: &buf[0]}
	b.data = i2cRdwrIoctlData{msgs: &b.msgs[0], nmsgs: uint32(len(b.msgs))}

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(b.fd), ioctlI2CRdwr, uintptr(unsafe.Pointer(&b.data)))
	runtime.KeepAlive(b)
	runtime.KeepAlive(buf)

	// Drop the reference to the caller's buffer.
	b.msgs[1].buf = nil
	if errno != 0 {
		return fmt.Errorf("i2c read 0x%02x reg 0x%02x: %w", addr, reg, errno)
	}
	return nil
}

// Close closes the device. Further reads fail.
func (b *LinuxBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fd < 0 {
		return errors.New("close i2c bus: already closed")
	}
	err := syscall.Close(b.fd)
	b.fd = -1
	if err != nil {
		return fmt.Errorf("close i2c bus: %w", err)
	}
	return nil
}
