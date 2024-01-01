//go:build !linux

package nat1

import "net"

func getSendQueueLength(conn *net.TCPConn) (qLen int) {
	return 0
}
