package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/libp2p/go-nat"
	"github.com/spf13/cobra"

	"github.com/koho/nat1"
)

var rootCmd = &cobra.Command{
	Use:   "nat1",
	Short: "Mapping the private IP address to public IP address in NAT1 network.",
}

var (
	localAddr string
	interval  time.Duration
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&localAddr, "local", "l", ":0", "local address to connect STUN server.")
	rootCmd.PersistentFlags().DurationVarP(&interval, "interval", "i", 50*time.Second, "connection keepalive interval")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func serve(network string, stun string) {
	var stunClient nat1.StunClient
	var err error
	if network == "tcp" {
		stunClient, err = nat1.NewStunTCPClient(localAddr, stun, keepaliveUrl)
	} else if network == "udp" {
		stunClient, err = nat1.NewStunUDPClient(localAddr, stun)
	} else {
		panic(fmt.Errorf("invalid network type: %s", network))
	}
	if err != nil {
		panic(err)
	}
	defer stunClient.Close()

	lAddr, rAddr, err := stunClient.MapAddress()
	if err != nil {
		panic(err)
	}

	log.Printf("[%s] new mapping from %s -> %s", network, lAddr.String(), rAddr.String())

	ka := time.NewTicker(interval)
	defer ka.Stop()
	for range ka.C {
		newLAddr, newRAddr, err := stunClient.MapAddress()
		if err != nil {
			log.Println(err)
			continue
		}
		if newLAddr.String() != lAddr.String() || newRAddr.String() != rAddr.String() {
			lAddr, rAddr = newLAddr, newRAddr
			log.Printf("[%s] new mapping from %s -> %s", network, lAddr.String(), rAddr.String())
		}
	}
}

func setupUPnP(network string, port int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	upnp := <-nat.DiscoverNATs(ctx)
	if upnp == nil {
		panic("no available upnp server in this network")
	}

	hostname, _ := os.Hostname()
	mappedPort, err := upnp.AddPortMapping(ctx, network, port, fmt.Sprintf("%s %d", hostname, port), 0)
	if err != nil {
		panic(err)
	}

	localAddr = net.JoinHostPort("0.0.0.0", strconv.Itoa(mappedPort))
	log.Printf("[%s] forwarding to port %d", network, port)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		if err = upnp.DeletePortMapping(context.Background(), network, port); err != nil {
			panic(err)
		}
		os.Exit(0)
	}()
}

func run(network string, stun string, args []string) {
	if len(args) > 0 {
		port, err := strconv.Atoi(args[0])
		if err != nil || port < 0 || port > 65535 {
			panic("port number must be between 0 to 65535")
		}
		setupUPnP(network, port)
	}
	serve(network, stun)
}
