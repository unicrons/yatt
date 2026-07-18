// Package scan orchestrates a single run: permute the seed, resolve every
// candidate, and collect the boolean signals into findings.
package scan

import (
	"context"
	"fmt"

	"github.com/andoniaf/yatt/internal/resolver"
	"github.com/andoniaf/yatt/pkg/engine"
)

// Finding is one candidate domain and everything currently known about it.
type Finding struct {
	Candidate   string   `json:"candidate"`
	Registrable string   `json:"registrable"`
	Technique   string   `json:"technique"`
	Registered  bool     `json:"registered"`
	HasNS       bool     `json:"has_ns"`
	HasA        bool     `json:"has_a"`
	HasMX       bool     `json:"has_mx"`
	Addresses   []string `json:"addresses,omitempty"`
	NS          []string `json:"ns,omitempty"`
	MX          []string `json:"mx,omitempty"`
	// Rcode is the response code of the NS query that decided Registered.
	Rcode string `json:"rcode,omitempty"`
	// Error records a per-candidate resolution failure. A failed candidate is
	// still reported rather than dropped, so a partially-failed scan is visibly
	// partial instead of silently short.
	Error string `json:"error,omitempty"`
}

// Options configures a run.
type Options struct {
	// Seed is the domain to permute.
	Seed string
	// Resolver answers the DNS questions.
	Resolver resolver.Resolver
	// Techniques are applied in the order given. Defaults to every registered
	// technique when empty.
	Techniques []engine.Technique
}

// Result is the outcome of a run.
type Result struct {
	Seed     engine.Seed `json:"-"`
	Findings []Finding   `json:"findings"`
}

// Run permutes the seed and resolves every candidate.
//
// Resolution is serial at this stage; bounded concurrency and rate limiting
// arrive with the wildcard work, once there is enough fan-out to need them.
func Run(ctx context.Context, opts Options) (Result, error) {
	if opts.Resolver == nil {
		return Result{}, fmt.Errorf("scan: no resolver configured")
	}

	seed, err := engine.ParseSeed(opts.Seed)
	if err != nil {
		return Result{}, err
	}

	techniques := opts.Techniques
	if len(techniques) == 0 {
		techniques = engine.All()
	}

	candidates := engine.Permute(seed, techniques)
	findings := make([]Finding, 0, len(candidates))

	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return Result{Seed: seed, Findings: findings}, err
		}

		finding := Finding{
			Candidate:   candidate.Domain,
			Registrable: candidate.Registrable,
			Technique:   candidate.Technique,
		}

		signals, err := resolver.Signals(ctx, opts.Resolver, candidate.Domain)
		if err != nil {
			finding.Error = err.Error()
		}
		finding.Registered = signals.Registered
		finding.HasNS = signals.HasNS
		finding.HasA = signals.HasA
		finding.HasMX = signals.HasMX
		finding.Addresses = signals.Addresses
		finding.NS = signals.NS
		finding.MX = signals.MX
		finding.Rcode = signals.Rcode

		findings = append(findings, finding)
	}

	return Result{Seed: seed, Findings: findings}, nil
}
