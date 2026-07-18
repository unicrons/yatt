package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/andoniaf/yatt/internal/resolver"
	"github.com/andoniaf/yatt/internal/scan"
	"github.com/andoniaf/yatt/internal/store"
)

// scriptedResolver answers NOERROR with an A record for the names in
// registered, and NXDOMAIN for everything else.
type scriptedResolver struct {
	registered map[string]bool
}

func (s scriptedResolver) Query(_ context.Context, name string, qtype uint16) (*dns.Msg, error) {
	m := new(dns.Msg)
	if !s.registered[strings.TrimSuffix(dns.Fqdn(name), ".")] {
		m.Rcode = dns.RcodeNameError
		return m, nil
	}
	m.Rcode = dns.RcodeSuccess
	switch qtype {
	case dns.TypeNS:
		m.Answer = append(m.Answer, &dns.NS{
			Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeNS},
			Ns:  "ns1.squatter.example.",
		})
	case dns.TypeA:
		m.Answer = append(m.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeA},
			A:   []byte{192, 0, 2, 1},
		})
	}
	return m, nil
}

// forbiddenResolver fails the test if it is asked anything at all.
type forbiddenResolver struct{ t *testing.T }

func (r forbiddenResolver) Query(_ context.Context, name string, _ uint16) (*dns.Msg, error) {
	r.t.Errorf("unexpected DNS query for %s: this command must answer from the store alone", name)
	return nil, errors.New("resolver must not be used")
}

// withFakeResolver swaps the resolver constructor for the duration of a test
// and reports the address the command asked for.
func withFakeResolver(t *testing.T, fake resolver.Resolver) *string {
	t.Helper()

	var requestedAddr string
	original := newResolver
	newResolver = func(addr string, _ time.Duration) (resolver.Resolver, error) {
		requestedAddr = addr
		return fake, nil
	}
	t.Cleanup(func() { newResolver = original })

	return &requestedAddr
}

// withForbiddenResolver asserts that a command reaches its answer without
// touching DNS.
//
// Both constructing a resolver and querying one fail the test: a command that
// dials the network only to discard the result is still wrong, and catching it
// at construction pins the failure to the line that caused it rather than to a
// query several frames deeper.
func withForbiddenResolver(t *testing.T) {
	t.Helper()

	original := newResolver
	newResolver = func(addr string, _ time.Duration) (resolver.Resolver, error) {
		t.Errorf("unexpected resolver constructed for %q: this command must answer from the store alone", addr)
		return forbiddenResolver{t: t}, nil
	}
	t.Cleanup(func() { newResolver = original })
}

// withTempStore points the command tree at a throwaway database, so no test
// ever reads or writes the user's real scan history.
func withTempStore(t *testing.T) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "yatt.db")
	original := newStore
	newStore = func(string) (store.Store, error) {
		return store.Open(path)
	}
	t.Cleanup(func() { newStore = original })
}

// run executes the command tree with args, returning stdout and stderr.
func run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestScanRendersTable(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"xample.com": true}})

	stdout, _, err := run(t, "scan", "example.com")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if !strings.Contains(stdout, "CANDIDATE") {
		t.Errorf("output has no header row:\n%s", stdout)
	}
	// "xample.com" is the omission of the leading "e" and is scripted as
	// registered; "eample.com" is not.
	if !strings.Contains(stdout, "xample.com") {
		t.Errorf("output is missing the xample.com candidate:\n%s", stdout)
	}
	if !strings.Contains(stdout, "eample.com") {
		t.Errorf("output is missing the eample.com candidate:\n%s", stdout)
	}
	if !strings.Contains(stdout, "192.0.2.1") {
		t.Errorf("output is missing the resolved address:\n%s", stdout)
	}
}

func TestScanRendersJSON(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"xample.com": true}})

	stdout, _, err := run(t, "scan", "example.com", "--output", "json")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	var findings []scan.Finding
	if err := json.Unmarshal([]byte(stdout), &findings); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	if len(findings) == 0 {
		t.Fatal("no findings")
	}

	// The seed's own row leads the report, so the candidates start at index 1.
	if findings[0].Technique != "original" || findings[0].Candidate != "example.com" {
		t.Errorf("first finding = %q/%q, want the seed row example.com/original",
			findings[0].Candidate, findings[0].Technique)
	}

	var registered int
	for _, f := range findings[1:] {
		if f.Technique != "omission" {
			t.Errorf("technique = %q, want %q", f.Technique, "omission")
		}
		if f.Registered {
			registered++
			if f.Candidate != "xample.com" {
				t.Errorf("unexpected registered candidate %q", f.Candidate)
			}
		}
	}
	if registered != 1 {
		t.Errorf("got %d registered findings, want 1", registered)
	}
}

func TestScanRendersNDJSON(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	stdout, _, err := run(t, "scan", "example.com", "--output", "ndjson")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	for i, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		var f scan.Finding
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}
}

func TestScanPassesResolverFlagThrough(t *testing.T) {
	withTempStore(t)
	addr := withFakeResolver(t, scriptedResolver{})

	if _, _, err := run(t, "scan", "example.com", "--resolver", "9.9.9.9:53"); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if *addr != "9.9.9.9:53" {
		t.Errorf("resolver address = %q, want %q", *addr, "9.9.9.9:53")
	}
}

func TestScanVerboseWritesToStderrOnly(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	stdout, stderr, err := run(t, "scan", "example.com", "--output", "json", "--verbose")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if !strings.Contains(stderr, "scanning example.com") {
		t.Errorf("stderr is missing the progress line:\n%s", stderr)
	}
	// Progress must never contaminate the machine-readable stream.
	var findings []scan.Finding
	if err := json.Unmarshal([]byte(stdout), &findings); err != nil {
		t.Errorf("stdout is not valid JSON with --verbose: %v\n%s", err, stdout)
	}
}

func TestScanErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "unknown output format",
			args: []string{"scan", "example.com", "--output", "yaml"},
			want: "unknown output format",
		},
		{
			name: "unparseable seed",
			args: []string{"scan", "not-a-domain"},
			want: "invalid domain",
		},
		{
			name: "missing argument",
			args: []string{"scan"},
			want: "accepts 1 arg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withTempStore(t)
			withFakeResolver(t, scriptedResolver{})

			_, _, err := run(t, tt.args...)
			if err == nil {
				t.Fatalf("run(%v) succeeded, want an error", tt.args)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}
