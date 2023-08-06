package ns

import (
	"strings"

	"github.com/miekg/dns"
)

type NS interface {
	SetA(domain string, value string) error
	SetSVCB(domain string, priority int, target string, params map[string]string) error
}

func SplitDomain(s string) (subdomain, domain string) {
	labels := dns.SplitDomainName(s)
	n := len(labels)
	if n < 2 {
		return "", s
	}
	domain = labels[n-2] + "." + labels[n-1]
	if labels[0] == "" {
		subdomain = strings.Join(labels[1:n-2], ".")
	} else {
		subdomain = strings.Join(labels[:n-2], ".")
	}
	return
}

func SplitDomainPtr(s string) (subdomain, domain *string) {
	subdomainStr, domainStr := SplitDomain(s)
	if subdomainStr != "" {
		subdomain = &subdomainStr
	}
	if domainStr != "" {
		domain = &domainStr
	}
	return
}

func GetRecord(domain string, t uint16, dnsServer string) (dns.RR, error) {
	c := new(dns.Client)
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), t)
	m.RecursionDesired = true
	r, _, err := c.Exchange(m, dnsServer)
	if err != nil {
		return nil, err
	}
	if len(r.Answer) == 0 {
		return nil, nil
	}
	return r.Answer[0], nil
}
