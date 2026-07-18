// Package scan orchestrates a single run: permute the seed, resolve every
// candidate, record the result, and compare it against the previous run.
package scan

import (
	"context"
	"fmt"

	"github.com/andoniaf/yatt/internal/resolver"
	"github.com/andoniaf/yatt/internal/store"
	"github.com/andoniaf/yatt/internal/triage"
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
	// Triage is the analyst's standing verdict on this candidate, carried
	// forward from every previous scan of the same seed. It is triage.StatusNew
	// for a candidate nobody has judged, and empty only when the run was not
	// persisted and so had no verdicts to read.
	Triage triage.Status `json:"triage,omitempty"`
	// TriageNote is the note recorded alongside the verdict.
	TriageNote string `json:"triage_note,omitempty"`
	// Diff is this candidate's standing against the previous scan of the same
	// seed. It is empty when the run was not persisted and so had nothing to
	// compare against.
	Diff DiffStatus `json:"diff,omitempty"`
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
	// Store persists the run and supplies the previous one to diff against.
	// When nil the scan is stateless: it still resolves and reports, but no
	// diff status is attached.
	Store store.Store
	// Profile names the settings the run used, recorded alongside the scan so
	// history explains why two scans of one seed differ in size.
	Profile string
}

// Result is the outcome of a run.
type Result struct {
	Seed engine.Seed `json:"-"`
	// ScanID identifies the persisted scan, or zero when the run was not
	// persisted.
	ScanID   int64     `json:"scan_id,omitempty"`
	Findings []Finding `json:"findings"`
	// Gone lists candidates the previous scan found and this one did not.
	Gone []Finding `json:"gone,omitempty"`
	// Previous is the scan this run was compared against, or nil if this is the
	// seed's first recorded scan.
	Previous *store.Scan `json:"previous,omitempty"`
}

// Run permutes the seed, resolves every candidate, records the run, and diffs
// it against the seed's previous run.
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

	// The seed leads its own report: it is resolved, recorded and diffed exactly
	// like a candidate, so an analyst can read the candidates' signals against
	// the real domain's instead of guessing what "normal" looks like for it.
	candidates := engine.WithOriginal(seed, engine.Permute(seed, techniques))
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

	result := Result{Seed: seed, Findings: findings}
	if opts.Store == nil {
		return result, nil
	}
	return persist(ctx, opts, seed.String(), result)
}

// persist records the run and attaches each finding's standing against the
// previous run.
//
// The previous scan is read before the new one is created, because the new scan
// would otherwise be the most recent and every candidate would compare against
// itself.
func persist(ctx context.Context, opts Options, seed string, result Result) (Result, error) {
	previous, priorFindings, err := opts.Store.LastScan(ctx, seed)
	if err != nil {
		return result, err
	}

	scanID, err := opts.Store.CreateScan(ctx, seed, opts.Profile)
	if err != nil {
		return result, err
	}
	if err := opts.Store.SaveFindings(ctx, scanID, toStore(result.Findings)); err != nil {
		return result, err
	}

	diff := Diff(fromStore(priorFindings), result.Findings)
	if err := hydrateTriage(ctx, opts.Store, seed, &diff); err != nil {
		return result, err
	}

	result.ScanID = scanID
	result.Findings = diff.Findings
	result.Gone = diff.Gone
	result.Previous = previous
	return result, nil
}

// hydrateTriage attaches each candidate's standing verdict to the findings.
//
// This runs after the diff rather than before it, because the diff is defined
// over resolved signals alone. A verdict is a statement by a human about a
// domain, not a property of the DNS answer, so changing one must never make a
// candidate report as "changed".
func hydrateTriage(ctx context.Context, s store.Store, seed string, diff *DiffResult) error {
	verdicts, err := s.GetTriage(ctx, seed)
	if err != nil {
		return err
	}

	apply := func(findings []Finding) {
		for i := range findings {
			verdict, ok := verdicts[store.NormalizeCandidate(findings[i].Candidate)]
			if !ok {
				if findings[i].IsOriginal() {
					// The seed's own row is left blank rather than "new". "New"
					// means "nobody has judged this yet", which is a statement
					// about an untriaged backlog — and the domain being
					// protected is not in anyone's backlog. Blanking it also
					// keeps `--status new` selecting exactly the candidates
					// still awaiting a verdict.
					continue
				}
				// An unjudged candidate is reported as new rather than blank, so
				// the column always says something and `--status new` selects
				// exactly the backlog.
				findings[i].Triage = triage.StatusNew
				continue
			}
			// A verdict deliberately recorded against the seed is still honoured:
			// marking your own domain `owned` is a reasonable thing to want to
			// see, and refusing to show it would make the record invisible.
			findings[i].Triage = verdict.Status
			findings[i].TriageNote = verdict.Note
		}
	}
	apply(diff.Findings)
	apply(diff.Gone)
	return nil
}

// Comparison is a diff between two recorded scans of one seed.
type Comparison struct {
	// Current and Previous are the two scans compared. Previous is nil when the
	// seed has only ever been scanned once.
	Current  *store.Scan `json:"current"`
	Previous *store.Scan `json:"previous"`
	// Result holds the classified findings.
	Result DiffResult `json:"-"`
}

// CompareLast diffs a seed's most recent recorded scan against the one before
// it, without resolving anything.
func CompareLast(ctx context.Context, s store.Store, seed string) (Comparison, error) {
	if s == nil {
		return Comparison{}, fmt.Errorf("scan: no store configured")
	}

	current, currentFindings, err := s.ScanAt(ctx, seed, 0)
	if err != nil {
		return Comparison{}, err
	}
	if current == nil {
		return Comparison{}, fmt.Errorf("no recorded scans for %s: run `yatt scan %s` first", seed, seed)
	}

	previous, priorFindings, err := s.ScanAt(ctx, seed, 1)
	if err != nil {
		return Comparison{}, err
	}

	// The diff reads only stored signals, but it still renders through the same
	// columns as a scan, so it carries the same verdicts.
	diff := Diff(fromStore(priorFindings), fromStore(currentFindings))
	if err := hydrateTriage(ctx, s, store.NormalizeSeed(seed), &diff); err != nil {
		return Comparison{}, err
	}

	return Comparison{
		Current:  current,
		Previous: previous,
		Result:   diff,
	}, nil
}
