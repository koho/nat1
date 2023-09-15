package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/koho/nat1"
	"github.com/koho/nat1/ns"
	"github.com/koho/nat1/ns/dnspod"
	"github.com/koho/nat1/pb"
)

var (
	defaultStunUDPServer     = "stun.qq.com:3478"
	defaultStunTCPServer     = "stun.xiaoyaoyou.xyz:3478"
	defaultKeepaliveUrl      = "http://connectivitycheck.platform.hicloud.com/generate_204"
	defaultDnsServer         = "114.114.114.114:53"
	defaultKeepaliveInterval = uint64(50)
)

func main() {
	cfgFile := "config.json"
	if len(os.Args) > 1 {
		cfgFile = os.Args[1]
	}
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		panic(err)
	}

	var cfg pb.Config
	if err = protojson.Unmarshal(data, &cfg); err != nil {
		panic(err)
	}
	completeConfig(&cfg)
	if err = cfg.Validate(); err != nil {
		panic(err)
	}

	var provider ns.NS
	switch v := cfg.Ns.(type) {
	case *pb.Config_Dnspod:
		provider, err = dnspod.New(v.Dnspod)
	}
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)
	go func() {
		<-c
		cancel()
	}()

	var wg sync.WaitGroup
	for i, svc := range cfg.Services {
		wg.Add(1)
		go func(i int, svc *pb.Service) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(rand.Intn(3)+5*i) * time.Second):
			}
			Serve(ctx, provider, &cfg, svc)
		}(i, svc)
	}
	wg.Wait()
}

func completeConfig(cfg *pb.Config) {
	if cfg.Stun == nil {
		cfg.Stun = new(pb.Stun)
	}
	if cfg.Stun.Tcp == nil {
		cfg.Stun.Tcp = new(pb.Stun_TCP)
	}
	if cfg.Stun.Tcp.Addr == "" {
		cfg.Stun.Tcp.Addr = defaultStunTCPServer
	}
	if cfg.Stun.Tcp.KeepaliveUrl == "" {
		cfg.Stun.Tcp.KeepaliveUrl = defaultKeepaliveUrl
	}
	if cfg.Stun.Udp == nil {
		cfg.Stun.Udp = new(pb.Stun_UDP)
	}
	if cfg.Stun.Udp.Addr == "" {
		cfg.Stun.Udp.Addr = defaultStunUDPServer
	}
	if cfg.Stun.Interval == nil {
		cfg.Stun.Interval = &defaultKeepaliveInterval
	}
	if cfg.Dns == nil {
		cfg.Dns = &defaultDnsServer
	}
	for _, svc := range cfg.Services {
		if svc.Network == "" {
			svc.Network = "udp"
		}
	}
}

func Serve(ctx context.Context, provider ns.NS, cfg *pb.Config, service *pb.Service) {
	defer log.Printf("[%s] service stopped", service.Domain)

	var stunClient nat1.StunClient
	var err error
	if service.Network == "tcp" {
		stunClient, err = nat1.NewStunTCPClient(service.Local, cfg.Stun.Tcp.Addr, cfg.Stun.Tcp.KeepaliveUrl)
	} else if service.Network == "udp" {
		stunClient, err = nat1.NewStunUDPClient(service.Local, cfg.Stun.Udp.Addr)
	} else {
		panic(fmt.Errorf("invalid network type: %s", service.Network))
	}
	if err != nil {
		panic(err)
	}
	defer stunClient.Close()

	if err = stunClient.AwaitConnection(ctx); err != nil {
		log.Printf("[%s] %s", service.Domain, err)
		return
	}
	_, rAddr, err := stunClient.MapAddress(ctx)
	if err != nil {
		log.Printf("[%s] %s", service.Domain, err)
		return
	}

	log.Printf("[%s] listening on %s", service.Domain, rAddr)

	svc := nat1.NewService(provider, service, *cfg.Dns, rAddr.Addr().String(), rAddr.Port())

	if err = svc.CompareAndUpdate(); err != nil {
		log.Printf("[%s] %s", service.Domain, err)
	}

	ka := time.NewTicker(time.Duration(*cfg.Stun.Interval) * time.Second)
	defer ka.Stop()
	hc := time.NewTicker(10 * time.Minute)
	defer hc.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ka.C:
			clientCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, rAddr, err = stunClient.MapAddress(clientCtx)
			cancel()
			if err != nil {
				log.Printf("[%s] %s", service.Domain, err)
				continue
			}
			if err = svc.Update(rAddr.Addr().String(), rAddr.Port()); err != nil {
				log.Printf("[%s] %s", service.Domain, err)
			}
		case <-hc.C:
			if err = svc.CompareAndUpdate(); err != nil {
				log.Printf("[%s] %s", service.Domain, err)
			}
		}
	}
}
