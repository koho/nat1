package nat1

import (
	"golang.org/x/sys/unix"
	"net"
	"unsafe"
)

func getSendQueueLength(conn *net.TCPConn) (qLen int) {
	if r, err := conn.SyscallConn(); err == nil {
		r.Control(func(fd uintptr) {
			unix.Syscall(unix.SYS_IOCTL, fd, unix.SIOCOUTQ, uintptr(unsafe.Pointer(&qLen)))
		})
	}
	return
}
