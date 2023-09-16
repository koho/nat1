package nat1

import (
	"log"
	"slices"
	"strconv"

	"github.com/miekg/dns"

	"github.com/koho/nat1/ns"
	"github.com/koho/nat1/pb"
)

type Service struct {
	*pb.Service
	provider  ns.NS
	dnsServer string
	alpn      string
	effective string
	ip        string
	port      uint16
}

func NewService(provider ns.NS, service *pb.Service, dnsServer string, ip string, port uint16) *Service {
	s := &Service{
		provider:  provider,
		Service:   service,
		dnsServer: dnsServer,
		ip:        ip,
		port:      port,
	}
	if s.Target == "" || s.Target == "." {
		s.Target = "."
		s.effective = s.Domain
	} else {
		s.effective = s.Target
	}
	if s.Priority == nil {
		var defaultPriority uint32 = 1
		s.Priority = &defaultPriority
	}
	s.alpn = (&dns.SVCBAlpn{Alpn: s.Alpn}).String()
	return s
}

func (s *Service) CompareAndUpdate() error {
	var other *dns.SVCB
	t := dns.TypeSVCB
	if s.Https {
		t = dns.TypeHTTPS
	}
	rr, err := ns.GetRecords(s.Domain, t, s.dnsServer)
	if err != nil {
		return err
	}
find:
	for _, v := range rr {
		var svcb *dns.SVCB
		switch v := v.(type) {
		case *dns.HTTPS:
			svcb = &v.SVCB
		case *dns.SVCB:
			svcb = v
		default:
			continue
		}
		if other == nil {
			other = svcb
		}
		for _, param := range svcb.Value {
			if alpn, ok := param.(*dns.SVCBAlpn); ok && slices.Equal(s.Alpn, alpn.Alpn) {
				other = svcb
				break find
			}
		}
	}
	if other == nil {
		other = new(dns.SVCB)
	}

	paramMatch := true
	ipv4Hint := ""
	var port uint16
	for _, v := range other.Value {
		switch v := v.(type) {
		case *dns.SVCBAlpn:
			if s.alpn != v.String() {
				paramMatch = false
			}
		case *dns.SVCBIPv4Hint:
			ipv4Hint = v.String()
			if !s.Hint || ipv4Hint != s.ip {
				paramMatch = false
			}
		case *dns.SVCBPort:
			port = v.Port
		default:
			if param, ok := s.Params[v.Key().String()]; ok && v.String() != param {
				paramMatch = false
			}
		}
	}
	if s.port != port || other.Target != dns.Fqdn(s.Target) || other.Priority != uint16(*s.Priority) || !paramMatch {
		log.Printf("[%s] [dns] updating SVCB record: %s:%d", s.Domain, s.ip, s.port)
		if err := s.provider.SetSVCB(
			s.Rid, s.Domain, int(*s.Priority), s.Target, s.makeSvcParams(), s.Https,
		); err != nil {
			return err
		}
	}
	if s.Hint && !s.A {
		return nil
	}
	rr, err = ns.GetRecords(s.effective, dns.TypeA, s.dnsServer)
	if err != nil {
		return err
	}
	if rr == nil || rr[0].(*dns.A).A.String() != s.ip {
		log.Printf("[%s] [dns] updating A record of %s: %s", s.Domain, s.effective, s.ip)
		return s.provider.SetA(s.Rid, s.effective, s.ip)
	}
	return nil
}

func (s *Service) Update(newIP string, newPort uint16) error {
	oldIP, oldPort := s.ip, s.port
	s.ip, s.port = newIP, newPort

	if oldPort != s.port || (s.Hint && oldIP != s.ip) {
		log.Printf("[%s] [stun] updating SVCB record: %s:%d", s.Domain, s.ip, s.port)
		if err := s.provider.SetSVCB(
			s.Rid, s.Domain, int(*s.Priority), s.Target, s.makeSvcParams(), s.Https,
		); err != nil {
			return err
		}
	}
	if (!s.Hint || s.A) && oldIP != s.ip {
		log.Printf("[%s] [stun] updating A record of %s: %s", s.Domain, s.effective, s.ip)
		return s.provider.SetA(s.Rid, s.effective, s.ip)
	}
	return nil
}

func (s *Service) makeSvcParams() map[string]string {
	r := make(map[string]string)
	for k, v := range s.Params {
		r[k] = v
	}
	if s.Hint {
		r["ipv4hint"] = s.ip
	}
	if s.alpn != "" {
		r["alpn"] = s.alpn
	}
	r["port"] = strconv.Itoa(int(s.port))
	return r
}
