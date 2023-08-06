package nat1s

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/libp2p/go-reuseport"
	"github.com/pion/stun"
)

const (
	waitTimeout = 5 * time.Second
)

type StunClient interface {
	io.Closer
	MapAddress() (net.IP, int, error)
}

func mappedAddress(msg *stun.Message) (net.IP, int, error) {
	var xorAddr stun.XORMappedAddress
	var mappedAddr stun.MappedAddress
	if err := mappedAddr.GetFrom(msg); err == nil {
		return mappedAddr.IP, mappedAddr.Port, nil
	}
	if err := xorAddr.GetFrom(msg); err == nil {
		return xorAddr.IP, xorAddr.Port, nil
	}
	return nil, 0, fmt.Errorf("no mapped address from stun server")
}

type StunUDPClient struct {
	*net.UDPConn
	server string
	ch     chan *stun.Message
	done   chan struct{}
}

func NewStunUDPClient(localAddr string, server string) (StunClient, error) {
	addr, err := net.ResolveUDPAddr("udp4", localAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, err
	}
	c := &StunUDPClient{
		UDPConn: conn,
		server:  server,
		ch:      make(chan *stun.Message),
		done:    make(chan struct{}),
	}
	go c.readUntilClosed()
	return c, nil
}

func (c *StunUDPClient) readUntilClosed() {
	defer close(c.done)
	for {
		m := new(stun.Message)
		m.Raw = make([]byte, 1024)
		tBuf := m.Raw[:cap(m.Raw)]
		n, _, err := c.ReadFromUDP(tBuf)
		if err != nil {
			return
		}
		m.Raw = tBuf[:n]
		if err = m.Decode(); err == nil {
			select {
			case c.ch <- m:
			default:
			}
		}
	}
}

func (c *StunUDPClient) MapAddress() (net.IP, int, error) {
	serverAddr, err := net.ResolveUDPAddr("udp4", c.server)
	if err != nil {
		return nil, 0, err
	}
	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	if _, err = c.WriteTo(message.Raw, serverAddr); err != nil {
		return nil, 0, err
	}

	select {
	case msg := <-c.ch:
		return mappedAddress(msg)
	case <-time.After(waitTimeout):
		return nil, 0, os.ErrDeadlineExceeded
	}
}

func (c *StunUDPClient) Close() error {
	err := c.UDPConn.Close()
	<-c.done
	return err
}

type StunTCPClient struct {
	ch        chan chan net.TCPAddr
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	dialer    *net.Dialer
	server    string
	kaAddr    string
	kaPayload []byte
}

func NewStunTCPClient(localAddr string, server string, keepaliveUrl string) (*StunTCPClient, error) {
	addr, err := net.ResolveTCPAddr("tcp4", localAddr)
	if err != nil {
		return nil, err
	}
	c := &StunTCPClient{
		dialer: &net.Dialer{Control: reuseport.Control, LocalAddr: addr},
		server: server,
		ch:     make(chan chan net.TCPAddr),
		done:   make(chan struct{}),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())

	u, err := url.Parse(keepaliveUrl)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" {
		return nil, fmt.Errorf("keepalive url only supports http scheme")
	}
	c.kaPayload = []byte(fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nConnection: keep-alive\r\n\r\n", u.RequestURI(), u.Host))
	c.kaAddr = u.Host
	if u.Port() == "" {
		c.kaAddr += ":80"
	}
	go c.keepalive()
	return c, nil
}

func (c *StunTCPClient) keepalive() {
	defer close(c.done)
	for {
		conn, err := c.dialer.DialContext(c.ctx, "tcp4", c.kaAddr)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Println(err)
			time.Sleep(waitTimeout)
			continue
		}
		done := make(chan struct{})

		ip, port, err := c.mapAddress()
		if err != nil {
			log.Println(err)
			goto next
		}

		go func(conn net.Conn) {
			defer close(done)
			if _, err := io.Copy(io.Discard, conn); err != nil {
				log.Println(err)
			}
		}(conn)

		for {
			select {
			case <-done:
				goto next
			case r := <-c.ch:
				r <- net.TCPAddr{IP: ip, Port: port}
				_, err = conn.Write(c.kaPayload)
				if err != nil {
					log.Println(err)
					goto next
				}
			case <-c.ctx.Done():
				conn.Close()
				return
			}
		}
	next:
		conn.Close()
		time.Sleep(waitTimeout)
	}
}

func (c *StunTCPClient) MapAddress() (net.IP, int, error) {
	addrCh := make(chan net.TCPAddr)
	select {
	case c.ch <- addrCh:
		addr := <-addrCh
		return addr.IP, addr.Port, nil
	case <-time.After(waitTimeout):
		return nil, 0, os.ErrDeadlineExceeded
	}
}

func (c *StunTCPClient) mapAddress() (net.IP, int, error) {
	conn, err := c.dialer.DialContext(c.ctx, "tcp4", c.server)
	if err != nil {
		return nil, 0, err
	}
	defer conn.Close()
	// Building binding request with random transaction id.
	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	if _, err = conn.Write(message.Raw); err != nil {
		return nil, 0, err
	}
	msg := new(stun.Message)
	msg.Raw = make([]byte, 1024)
	tBuf := msg.Raw[:cap(msg.Raw)]
	n, err := conn.Read(tBuf)
	if err != nil {
		return nil, 0, err
	}
	msg.Raw = tBuf[:n]
	if err = msg.Decode(); err != nil {
		return nil, 0, err
	}
	return mappedAddress(msg)
}

func (c *StunTCPClient) Close() error {
	c.cancel()
	<-c.done
	return nil
}
