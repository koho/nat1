package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
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
	rootCmd.PersistentFlags().StringVarP(&localAddr, "local", "l", ":0", "local address to connect STUN server")
	rootCmd.PersistentFlags().DurationVarP(&interval, "interval", "i", 50*time.Second, "connection keepalive interval")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func serve(ctx context.Context, network string, stun string) error {
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
		return err
	}
	defer stunClient.Close()

	clientCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err = stunClient.AwaitConnection(clientCtx); err != nil {
		return err
	}
	lAddr, rAddr, err := stunClient.MapAddress(clientCtx)
	if err != nil {
		return err
	}

	log.Printf("[%s] new mapping from %s -> %s", network, lAddr.String(), rAddr.String())

	ka := time.NewTicker(interval)
	defer ka.Stop()
	for {
		select {
		case <-ka.C:
			clientCtx, cancel = context.WithTimeout(ctx, 5*time.Second)
			newLAddr, newRAddr, err := stunClient.MapAddress(clientCtx)
			cancel()
			if err != nil {
				log.Println(err)
				continue
			}
			if newLAddr.String() != lAddr.String() || newRAddr.String() != rAddr.String() {
				lAddr, rAddr = newLAddr, newRAddr
				log.Printf("[%s] new mapping from %s -> %s", network, lAddr.String(), rAddr.String())
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func setupUPnP(network string, port int, description string) nat.NAT {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	upnp := <-nat.DiscoverNATs(ctx)
	if upnp == nil {
		panic("no available upnp server in this network")
	}

	if description == "" {
		hostname, _ := os.Hostname()
		description = fmt.Sprintf("NAT1 %s %d, %s", strings.ToUpper(network), port, hostname)
	}
	mappedPort, err := upnp.AddPortMapping(ctx, network, port, description, 0)
	if err != nil {
		panic(err)
	}

	localAddr = net.JoinHostPort("0.0.0.0", strconv.Itoa(mappedPort))
	log.Printf("[%s] forwarding to port %d", network, port)
	return upnp
}

func run(network string, stun string, args []string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleanup := cancel
	if len(args) > 0 {
		port, err := strconv.Atoi(args[0])
		if err != nil || port < 0 || port > 65535 {
			panic("port number must be between 0 to 65535")
		}
		var description string
		if len(args) > 1 {
			description = args[1]
		}
		upnp := setupUPnP(network, port, description)
		cleanup = func() {
			if err = upnp.DeletePortMapping(context.Background(), network, port); err != nil {
				log.Println(err)
			}
			cancel()
		}
	}
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Printf("[%s] closing connections", network)
		cleanup()
	}()
	if err := serve(ctx, network, stun); err != nil && !errors.Is(err, context.Canceled) {
		log.Println(err)
	}
}
