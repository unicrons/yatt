// Package resolver wraps github.com/miekg/dns with just enough surface for
// bulk look-alike scanning.
//
// miekg/dns is used rather than the standard library's net.Resolver because
// only it exposes the raw response Rcode. net.DNSError.IsNotFound conflates a
// true NXDOMAIN with a NOERROR-but-empty answer, which would erase the single
// most important signal this tool reports: whether a candidate is registered.
package resolver

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/miekg/dns"

	"github.com/andoniaf/yatt/pkg/engine"
)

// DefaultResolver is used when no resolver could be read from the system
// configuration and none was given on the command line.
const DefaultResolver = "1.1.1.1:53"

// DefaultTimeout bounds a single query.
const DefaultTimeout = 3 * time.Second

// Resolver issues a single DNS question and returns the raw response.
//
// It is an interface so the scan path can be exercised against scripted
// responses, with no network involved.
type Resolver interface {
	Query(ctx context.Context, name string, qtype uint16) (*dns.Msg, error)
}

// Client is the live Resolver, talking to a single upstream resolver.
type Client struct {
	addr   string
	client *dns.Client
}

// New returns a Client querying addr. A missing port defaults to 53. An empty
// addr falls back to the system resolver.
func New(addr string, timeout time.Duration) (*Client, error) {
	if addr == "" {
		addr = SystemResolver()
	}
	addr, err := normalizeAddr(addr)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{addr: addr, client: &dns.Client{Timeout: timeout}}, nil
}

// Addr returns the upstream resolver address in use.
func (c *Client) Addr() string { return c.addr }

// Query implements Resolver.
//
// Note that miekg/dns neither retries nor falls back to TCP on a truncated
// response; both are the caller's responsibility and are added in a later
// phase.
func (c *Client) Query(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	m.RecursionDesired = true

	resp, _, err := c.client.ExchangeContext(ctx, m, c.addr)
	if err != nil {
		return nil, fmt.Errorf("query %s %s: %w", dns.TypeToString[qtype], name, err)
	}
	return resp, nil
}

// SystemResolver returns the first nameserver from the host's resolver
// configuration, or DefaultResolver if it cannot be read.
func SystemResolver() string {
	cfg, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil || len(cfg.Servers) == 0 {
		return DefaultResolver
	}
	port := cfg.Port
	if port == "" {
		port = "53"
	}
	return net.JoinHostPort(cfg.Servers[0], port)
}

// normalizeAddr appends the default DNS port when addr carries none.
func normalizeAddr(addr string) (string, error) {
	if addr == "" {
		return "", fmt.Errorf("empty resolver address")
	}
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr, nil
	}
	return net.JoinHostPort(addr, "53"), nil
}

// Result is the boolean signal set for a single candidate domain.
type Result struct {
	// Domain is the name that was queried.
	Domain string
	// Registrable is the eTLD+1 the NS query targeted.
	Registrable string
	// Registered is true when the NS query at Registrable answered anything
	// other than NXDOMAIN.
	Registered bool
	// Rcode is the textual response code of that NS query, e.g. "NOERROR".
	Rcode string
	// HasNS, HasA and HasMX report record presence, which is deliberately kept
	// separate from Registered: a parked or delegation-only domain is
	// registered while having no A record at all.
	HasNS bool
	HasA  bool
	HasMX bool

	Addresses []string
	MX        []string
	NS        []string
}

// Signals resolves the boolean signal set for domain.
//
// Registration is decided by the Rcode of an NS query at the registrable
// domain, not by an A-record NXDOMAIN. The naive A-record test produces false
// negatives on exactly the MX-only and delegation-only domains this tool exists
// to surface — parked and defensive registrations.
//
// Unregistered names short-circuit: there is nothing to ask about a name that
// does not exist, and they are the overwhelming majority of any candidate set.
func Signals(ctx context.Context, r Resolver, domain string) (Result, error) {
	seed, err := engine.ParseSeed(domain)
	if err != nil {
		return Result{}, err
	}
	registrable := seed.Registrable()
	res := Result{Domain: seed.String(), Registrable: registrable}

	nsResp, err := r.Query(ctx, registrable, dns.TypeNS)
	if err != nil {
		return res, err
	}
	res.Rcode = rcodeString(nsResp.Rcode)

	switch nsResp.Rcode {
	case dns.RcodeNameError:
		return res, nil
	case dns.RcodeSuccess:
		res.Registered = true
	default:
		return res, fmt.Errorf("NS %s: %s", registrable, res.Rcode)
	}

	for _, rr := range nsResp.Answer {
		if ns, ok := rr.(*dns.NS); ok {
			res.HasNS = true
			res.NS = append(res.NS, trimDot(ns.Ns))
		}
	}

	for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA} {
		resp, err := r.Query(ctx, res.Domain, qtype)
		if err != nil {
			return res, err
		}
		for _, rr := range resp.Answer {
			switch record := rr.(type) {
			case *dns.A:
				res.HasA = true
				res.Addresses = append(res.Addresses, record.A.String())
			case *dns.AAAA:
				res.HasA = true
				res.Addresses = append(res.Addresses, record.AAAA.String())
			}
		}
	}

	mxResp, err := r.Query(ctx, res.Domain, dns.TypeMX)
	if err != nil {
		return res, err
	}
	for _, rr := range mxResp.Answer {
		if mx, ok := rr.(*dns.MX); ok {
			res.HasMX = true
			res.MX = append(res.MX, trimDot(mx.Mx))
		}
	}

	return res, nil
}

func rcodeString(rcode int) string {
	if s, ok := dns.RcodeToString[rcode]; ok {
		return s
	}
	return fmt.Sprintf("RCODE%d", rcode)
}

func trimDot(name string) string {
	if len(name) > 1 && name[len(name)-1] == '.' {
		return name[:len(name)-1]
	}
	return name
}
