package resolver_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/miekg/dns"

	"github.com/andoniaf/yatt/internal/resolver"
)

// question keys a scripted response by name and query type.
type question struct {
	name  string
	qtype uint16
}

// fakeResolver answers from a script, with NXDOMAIN as the default for any
// question not covered. It records every question asked so tests can assert
// which queries were issued.
type fakeResolver struct {
	responses map[question]*dns.Msg
	err       error
	asked     []question
}

func (f *fakeResolver) Query(_ context.Context, name string, qtype uint16) (*dns.Msg, error) {
	q := question{name: dns.Fqdn(name), qtype: qtype}
	f.asked = append(f.asked, q)
	if f.err != nil {
		return nil, f.err
	}
	if msg, ok := f.responses[q]; ok {
		return msg, nil
	}
	return msgWithRcode(dns.RcodeNameError), nil
}

func msgWithRcode(rcode int) *dns.Msg {
	m := new(dns.Msg)
	m.Rcode = rcode
	return m
}

func nsMsg(zone string, nameservers ...string) *dns.Msg {
	m := msgWithRcode(dns.RcodeSuccess)
	for _, ns := range nameservers {
		m.Answer = append(m.Answer, &dns.NS{
			Hdr: dns.RR_Header{Name: dns.Fqdn(zone), Rrtype: dns.TypeNS},
			Ns:  dns.Fqdn(ns),
		})
	}
	return m
}

func aMsg(name string, addresses ...string) *dns.Msg {
	m := msgWithRcode(dns.RcodeSuccess)
	for _, addr := range addresses {
		m.Answer = append(m.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeA},
			A:   mustIP(addr),
		})
	}
	return m
}

func mxMsg(name string, hosts ...string) *dns.Msg {
	m := msgWithRcode(dns.RcodeSuccess)
	for _, host := range hosts {
		m.Answer = append(m.Answer, &dns.MX{
			Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeMX},
			Mx:  dns.Fqdn(host),
		})
	}
	return m
}

func mustIP(s string) net.IP {
	ip := net.ParseIP(s)
	if ip == nil {
		panic("bad test IP " + s)
	}
	return ip
}

func TestSignalsUnregistered(t *testing.T) {
	fake := &fakeResolver{} // everything NXDOMAIN

	got, err := resolver.Signals(context.Background(), fake, "example.com")
	if err != nil {
		t.Fatalf("Signals: %v", err)
	}

	if got.Registered {
		t.Error("Registered = true, want false for an NXDOMAIN NS response")
	}
	if got.Rcode != "NXDOMAIN" {
		t.Errorf("Rcode = %q, want %q", got.Rcode, "NXDOMAIN")
	}
	// An unregistered name must short-circuit: asking for A/AAAA/MX on a name
	// that does not exist is wasted budget on the majority of every scan.
	if len(fake.asked) != 1 {
		t.Errorf("issued %d queries, want 1: %v", len(fake.asked), fake.asked)
	}
}

func TestSignalsRegisteredWithoutRecords(t *testing.T) {
	// The delegation-only case: NOERROR at the registrable domain with no NS
	// records in the answer section, and nothing else resolving. A tool that
	// inferred registration from an A-record NXDOMAIN would miss this.
	fake := &fakeResolver{responses: map[question]*dns.Msg{
		{name: "example.com.", qtype: dns.TypeNS}: msgWithRcode(dns.RcodeSuccess),
	}}

	got, err := resolver.Signals(context.Background(), fake, "example.com")
	if err != nil {
		t.Fatalf("Signals: %v", err)
	}

	if !got.Registered {
		t.Error("Registered = false, want true for a NOERROR NS response")
	}
	if got.HasA || got.HasMX || got.HasNS {
		t.Errorf("record presence = NS:%v A:%v MX:%v, want all false", got.HasNS, got.HasA, got.HasMX)
	}
}

func TestSignalsRegisteredMXOnly(t *testing.T) {
	// The other false-negative case naive tools produce: mail is served, the
	// web is not. Registered must be true and HasA false, independently.
	fake := &fakeResolver{responses: map[question]*dns.Msg{
		{name: "example.com.", qtype: dns.TypeNS}: nsMsg("example.com", "ns1.example.net"),
		{name: "example.com.", qtype: dns.TypeMX}: mxMsg("example.com", "mail.example.net"),
	}}

	got, err := resolver.Signals(context.Background(), fake, "example.com")
	if err != nil {
		t.Fatalf("Signals: %v", err)
	}

	if !got.Registered {
		t.Error("Registered = false, want true")
	}
	if !got.HasNS {
		t.Error("HasNS = false, want true")
	}
	if !got.HasMX {
		t.Error("HasMX = false, want true")
	}
	if got.HasA {
		t.Error("HasA = true, want false")
	}
	if len(got.MX) != 1 || got.MX[0] != "mail.example.net" {
		t.Errorf("MX = %v, want [mail.example.net]", got.MX)
	}
	if len(got.NS) != 1 || got.NS[0] != "ns1.example.net" {
		t.Errorf("NS = %v, want [ns1.example.net]", got.NS)
	}
}

func TestSignalsFullyResolving(t *testing.T) {
	fake := &fakeResolver{responses: map[question]*dns.Msg{
		{name: "example.com.", qtype: dns.TypeNS}: nsMsg("example.com", "ns1.example.net"),
		{name: "example.com.", qtype: dns.TypeA}:  aMsg("example.com", "93.184.216.34"),
		{name: "example.com.", qtype: dns.TypeMX}: mxMsg("example.com", "mail.example.net"),
	}}

	got, err := resolver.Signals(context.Background(), fake, "example.com")
	if err != nil {
		t.Fatalf("Signals: %v", err)
	}

	if !got.Registered || !got.HasNS || !got.HasA || !got.HasMX {
		t.Errorf("signals = registered:%v NS:%v A:%v MX:%v, want all true",
			got.Registered, got.HasNS, got.HasA, got.HasMX)
	}
	if len(got.Addresses) != 1 || got.Addresses[0] != "93.184.216.34" {
		t.Errorf("Addresses = %v, want [93.184.216.34]", got.Addresses)
	}
}

func TestSignalsQueriesTheRegistrableDomain(t *testing.T) {
	// The NS query must target the eTLD+1, never the full name and never a
	// public suffix: "co.uk" always answers NOERROR, which would mark every
	// candidate registered.
	fake := &fakeResolver{}

	got, err := resolver.Signals(context.Background(), fake, "www.example.co.uk")
	if err != nil {
		t.Fatalf("Signals: %v", err)
	}

	if got.Registrable != "example.co.uk" {
		t.Errorf("Registrable = %q, want %q", got.Registrable, "example.co.uk")
	}
	if len(fake.asked) == 0 {
		t.Fatal("no queries issued")
	}
	if fake.asked[0].name != "example.co.uk." || fake.asked[0].qtype != dns.TypeNS {
		t.Errorf("first query = %v, want NS example.co.uk.", fake.asked[0])
	}
}

func TestSignalsServerFailureIsAnError(t *testing.T) {
	// SERVFAIL says nothing about registration; reporting it as unregistered
	// would be a silent false negative.
	fake := &fakeResolver{responses: map[question]*dns.Msg{
		{name: "example.com.", qtype: dns.TypeNS}: msgWithRcode(dns.RcodeServerFailure),
	}}

	got, err := resolver.Signals(context.Background(), fake, "example.com")
	if err == nil {
		t.Fatal("Signals succeeded, want an error for SERVFAIL")
	}
	if got.Registered {
		t.Error("Registered = true, want false")
	}
}

func TestSignalsTransportErrorPropagates(t *testing.T) {
	fake := &fakeResolver{err: errors.New("i/o timeout")}

	if _, err := resolver.Signals(context.Background(), fake, "example.com"); err == nil {
		t.Fatal("Signals succeeded, want the transport error")
	}
}

func TestSignalsInvalidDomain(t *testing.T) {
	fake := &fakeResolver{}

	if _, err := resolver.Signals(context.Background(), fake, "not-a-domain"); err == nil {
		t.Fatal("Signals succeeded, want a parse error")
	}
	if len(fake.asked) != 0 {
		t.Errorf("issued %d queries for an unparseable name, want 0", len(fake.asked))
	}
}

func TestNewNormalizesResolverAddress(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{"1.1.1.1", "1.1.1.1:53"},
		{"1.1.1.1:5353", "1.1.1.1:5353"},
	}

	for _, tt := range tests {
		client, err := resolver.New(tt.addr, 0)
		if err != nil {
			t.Fatalf("New(%q): %v", tt.addr, err)
		}
		if got := client.Addr(); got != tt.want {
			t.Errorf("Addr() = %q, want %q", got, tt.want)
		}
	}
}

func TestNewFallsBackToSystemResolver(t *testing.T) {
	client, err := resolver.New("", 0)
	if err != nil {
		t.Fatalf("New(\"\"): %v", err)
	}
	if client.Addr() == "" {
		t.Error("Addr() is empty, want a resolver address")
	}
}
