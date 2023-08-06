package main

import (
	"fmt"
	"os"

	"github.com/miekg/dns"

	"github.com/koho/nat1s/ns"
)

const defaultDnsServer = "114.114.114.114:53"

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: svca domain")
		os.Exit(1)
	}
	record, err := ns.GetRecord(os.Args[1], dns.TypeSVCB, defaultDnsServer)
	if err != nil {
		panic(err)
	}
	if record == nil {
		fmt.Println("record not found")
		os.Exit(1)
	}
	var ip string
	var port uint16
	rr := record.(*dns.SVCB)
	for _, v := range rr.Value {
		switch v := v.(type) {
		case *dns.SVCBIPv4Hint:
			ip = v.Hint[0].String()
		case *dns.SVCBPort:
			port = v.Port
		case *dns.SVCBAlpn:
		}
	}
	if port == 0 {
		fmt.Println("port not found")
		os.Exit(1)
	}
	if ip == "" {
		ip = rr.Target
	}
	fmt.Printf("%s:%d\n", ip, port)
}
