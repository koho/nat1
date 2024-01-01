package nat1

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"net/url"
	"sync"
	"time"

	"github.com/libp2p/go-reuseport"
	"github.com/pion/stun"
)

const waitTime = 5 * time.Second

type StunClient interface {
	io.Closer
	AwaitConnection(ctx context.Context) error
	MapAddress(ctx context.Context) (lAddr, rAddr netip.AddrPort, err error)
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

func (c *StunUDPClient) MapAddress(ctx context.Context) (lAddr, rAddr netip.AddrPort, err error) {
	serverAddr, err := net.ResolveUDPAddr("udp4", c.server)
	if err != nil {
		return
	}
	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	if _, err = c.WriteTo(message.Raw, serverAddr); err != nil {
		return
	}

	select {
	case msg := <-c.ch:
		var ip net.IP
		var port int
		ip, port, err = mappedAddress(msg)
		if err != nil {
			return
		}
		if ip, ok := netip.AddrFromSlice(ip); ok {
			lAddr = c.LocalAddr().(*net.UDPAddr).AddrPort()
			rAddr = netip.AddrPortFrom(ip, uint16(port))
		} else {
			err = stun.ErrBadIPLength
		}
		return
	case <-ctx.Done():
		err = ctx.Err()
		return
	}
}

func (c *StunUDPClient) AwaitConnection(ctx context.Context) error {
	return nil
}

func (c *StunUDPClient) Close() error {
	err := c.UDPConn.Close()
	<-c.done
	return err
}

type StunTCPClient struct {
	ch        chan chan [2]net.TCPAddr
	ctx       context.Context
	cancel    context.CancelFunc
	connUp    chan struct{}
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
		dialer: &net.Dialer{Control: reuseport.Control, LocalAddr: addr, Timeout: 10 * time.Second, KeepAlive: -1},
		server: server,
		ch:     make(chan chan [2]net.TCPAddr),
		done:   make(chan struct{}),
		connUp: make(chan struct{}),
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
	go c.run()
	return c, nil
}

func (c *StunTCPClient) run() {
	defer close(c.done)

	var err error
	var conn net.Conn
	var lAddr, rAddr *net.TCPAddr
	var once sync.Once
	var readDone chan struct{}

	closeConn := func() {
		if conn != nil {
			conn.Close()
			conn = nil
		}
	}
	defer closeConn()

	timer := time.NewTimer(0)
	for {
		select {
		case <-readDone:
			readDone = nil
			closeConn()
			timer.Reset(waitTime)
		case <-c.ctx.Done():
			return
		case <-timer.C:
			var ip net.IP
			var port int
			conn, err = c.dialer.DialContext(c.ctx, "tcp4", c.kaAddr)
			if err != nil {
				log.Println(err)
				goto retry
			}
			lAddr = conn.LocalAddr().(*net.TCPAddr)
			c.dialer.LocalAddr = lAddr

			// Get the mapped public address
			ip, port, err = c.mapAddress()
			if err != nil {
				log.Println(err)
				closeConn()
				goto retry
			}
			rAddr = &net.TCPAddr{IP: ip, Port: port}

			// Discard all received data
			readDone = make(chan struct{})
			go func(conn net.Conn) {
				defer close(readDone)
				if _, err := io.Copy(io.Discard, conn); err != nil && c.ctx.Err() == nil {
					log.Println(err)
				}
			}(conn)
			// Notify others that the connection is ready
			once.Do(func() {
				close(c.connUp)
			})
			continue
		retry:
			timer.Reset(waitTime)
		case r := <-c.ch:
			if lAddr != nil && rAddr != nil {
				r <- [2]net.TCPAddr{*lAddr, *rAddr}
			} else {
				// The mapped address may not be available
				r <- [2]net.TCPAddr{}
			}
			if conn == nil {
				continue
			}
			// Send a keepalive payload to remote server to
			// keep the connection alive.
			if err = conn.SetWriteDeadline(time.Now().Add(waitTime)); err != nil {
				log.Println(err)
			}
			if _, err = conn.Write(c.kaPayload); err != nil {
				log.Println(err)
				closeConn()
				timer.Reset(waitTime)
			}
		}
	}
}

func (c *StunTCPClient) MapAddress(ctx context.Context) (lAddr, rAddr netip.AddrPort, err error) {
	addrCh := make(chan [2]net.TCPAddr)
	select {
	case c.ch <- addrCh:
		addr := <-addrCh
		if addr[1].IP == nil {
			err = fmt.Errorf("connection with the server is currently down")
			return
		}
		return addr[0].AddrPort(), addr[1].AddrPort(), nil
	case <-ctx.Done():
		err = ctx.Err()
		return
	}
}

func (c *StunTCPClient) AwaitConnection(ctx context.Context) error {
	select {
	case <-c.connUp:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done: // If connection process is cancelled we should exit
		return fmt.Errorf("connection shutting down")
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

	if err = conn.SetWriteDeadline(time.Now().Add(waitTime)); err != nil {
		return nil, 0, err
	}
	if _, err = conn.Write(message.Raw); err != nil {
		return nil, 0, err
	}
	msg := new(stun.Message)
	msg.Raw = make([]byte, 1024)
	tBuf := msg.Raw[:cap(msg.Raw)]
	if err = conn.SetReadDeadline(time.Now().Add(waitTime)); err != nil {
		return nil, 0, err
	}
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
