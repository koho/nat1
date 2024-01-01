package nat1

import (
	"log"
	"os/exec"
)

func init() {
	if err := exec.Command("sysctl", "-w", "net.ipv4.tcp_retries2=8").Run(); err != nil {
		log.Println(err)
	}
}
