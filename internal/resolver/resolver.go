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
	"golang.org/x/time/rate"

	"github.com/andoniaf/yatt/pkg/engine"
)

// DefaultResolver is used when no resolver could be read from the system
// configuration and none was given on the command line.
const DefaultResolver = "1.1.1.1:53"

// DefaultTimeout bounds a single query.
const DefaultTimeout = 3 * time.Second

// DefaultAttempts is how many times a query is sent before giving up, counting
// the first send. Scans are overwhelmingly NXDOMAIN, which is exactly the
// traffic public resolvers throttle by dropping packets, so a single timeout is
// weak evidence that a name does not resolve.
const DefaultAttempts = 3

// DefaultBackoff is the pause before the second attempt; it doubles thereafter.
const DefaultBackoff = 250 * time.Millisecond

// Resolver issues a single DNS question and returns the raw response.
//
// It is an interface so the scan path can be exercised against scripted
// responses, with no network involved.
type Resolver interface {
	Query(ctx context.Context, name string, qtype uint16) (*dns.Msg, error)
}

// Client is the live Resolver, talking to a single upstream resolver.
//
// miekg/dns does neither of the two things a bulk scanner needs from a DNS
// client: it never retries, and it never falls back to TCP when a response
// comes back truncated. Both are done here, because both failure modes are
// silent — a dropped UDP packet and a truncated answer would otherwise both
// read as "this candidate does not exist".
type Client struct {
	addr string
	udp  *dns.Client
	tcp  *dns.Client
	// attempts counts the first send, so 1 means no retry.
	attempts int
	backoff  time.Duration
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
	return &Client{
		addr:     addr,
		udp:      &dns.Client{Timeout: timeout},
		tcp:      &dns.Client{Net: "tcp", Timeout: timeout},
		attempts: DefaultAttempts,
		backoff:  DefaultBackoff,
	}, nil
}

// Addr returns the upstream resolver address in use.
func (c *Client) Addr() string { return c.addr }

// Query implements Resolver.
//
// A transport failure is retried with exponential backoff; a truncated
// response is re-asked over TCP. A response carrying an Rcode is returned as
// it stands, whatever that Rcode is: deciding which response codes mean
// something is the caller's job, and re-asking a resolver that just answered
// SERVFAIL mostly adds load to a resolver already struggling.
func (c *Client) Query(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	m.RecursionDesired = true

	backoff := c.backoff
	var lastErr error
	for attempt := 0; attempt < c.attempts; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, backoff); err != nil {
				return nil, err
			}
			backoff *= 2
		}

		resp, _, err := c.udp.ExchangeContext(ctx, m, c.addr)
		if err != nil {
			lastErr = err
			// A cancelled or expired context will not recover on a retry, and
			// retrying it would mask the reason the scan stopped.
			if ctx.Err() != nil {
				break
			}
			continue
		}
		if !resp.Truncated {
			return resp, nil
		}

		// Truncation means the answer exists but did not fit in a UDP datagram.
		// Returning it as-is would under-report records; TCP has no such limit.
		resp, _, err = c.tcp.ExchangeContext(ctx, m, c.addr)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				break
			}
			continue
		}
		return resp, nil
	}

	return nil, fmt.Errorf("query %s %s: %w", dns.TypeToString[qtype], name, lastErr)
}

// sleep waits for d unless the context ends first.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// DefaultQPS is the query-per-second ceiling applied when none is configured.
// Zero means unlimited: the throttle that bites first is usually the upstream
// resolver's own, and guessing a ceiling on the user's behalf would slow every
// scan to protect a resolver that may well be a local unbound.
const DefaultQPS = 0

// RateLimited wraps r so that no more than qps queries leave per second. A qps
// of zero or less returns r unchanged.
//
// The ceiling is applied by wrapping the Resolver rather than by pacing the
// scan loop, so every query counts against it — including the wildcard probes,
// which are issued from somewhere the scan loop cannot see.
func RateLimited(r Resolver, qps float64) Resolver {
	if qps <= 0 {
		return r
	}
	// A burst of one keeps the spacing even. A larger burst would let a scan
	// open with a spike at exactly the moment a resolver is most likely to
	// start dropping it.
	return &limitedResolver{inner: r, limiter: rate.NewLimiter(rate.Limit(qps), 1)}
}

type limitedResolver struct {
	inner   Resolver
	limiter *rate.Limiter
}

func (l *limitedResolver) Query(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	if err := l.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("query %s %s: %w", dns.TypeToString[qtype], name, err)
	}
	return l.inner.Query(ctx, name, qtype)
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
