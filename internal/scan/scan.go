// Package scan orchestrates a single run: permute the seed, resolve every
// candidate, record the result, and compare it against the previous run.
package scan

import (
	"context"
	"fmt"
	"sort"

	"golang.org/x/sync/errgroup"

	"github.com/andoniaf/yatt/internal/resolver"
	"github.com/andoniaf/yatt/internal/store"
	"github.com/andoniaf/yatt/internal/triage"
	"github.com/andoniaf/yatt/internal/wildcard"
	"github.com/andoniaf/yatt/pkg/engine"
)

// DefaultConcurrency is how many candidates are resolved at once when no limit
// is configured. It is deliberately modest: the resolver, not this process, is
// the thing that falls over first, and a scan that gets itself throttled
// finishes later than one that never was.
const DefaultConcurrency = 20

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
	// Wildcard reports that this candidate's addresses are indistinguishable
	// from what its zone hands out for names that do not exist. The signals are
	// still reported as resolved, because they are what DNS said; the flag is
	// what stops an analyst reading a catch-all zone as a thousand live
	// look-alikes.
	Wildcard bool `json:"wildcard,omitempty"`
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
	// Concurrency bounds how many candidates are resolved at once. Defaults to
	// DefaultConcurrency when zero or negative.
	Concurrency int
	// QPS caps how many DNS queries leave per second, across all workers. Zero
	// means unlimited.
	QPS float64
	// TLDProfile names the TLD list the tld technique swaps against ("common"
	// or "full"), when TLD swap is selected and TLDs is not set. Empty
	// defaults to "common".
	TLDProfile string
	// TLDs overrides TLDProfile with an explicit TLD list, e.g. read from
	// --tld-file. The seed's own suffix is still excluded automatically.
	TLDs []string
	// Limit bounds the total candidate count after per-technique capping.
	// Zero or negative means unlimited.
	Limit int
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
// Candidates are resolved concurrently under a worker bound and an optional
// QPS ceiling, but the report they produce does not depend on that: each worker
// writes into its own slot in a slice ordered by the permutation engine, so two
// runs over the same answers render byte for byte identically. That is not a
// nicety — the entire diff feature rests on candidate order being a property of
// the seed rather than of which goroutine happened to finish first.
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
	// TLD swap needs the seed's own suffix to resolve its profile, so this
	// happens after ParseSeed rather than being the caller's job — a caller
	// only needs to say "common" or "full", not know what that means for
	// this particular seed.
	techniques, err = engine.ResolveTLDs(techniques, seed.Suffix, opts.TLDProfile, opts.TLDs)
	if err != nil {
		return Result{}, err
	}

	// The seed leads its own report: it is resolved, recorded and diffed exactly
	// like a candidate, so an analyst can read the candidates' signals against
	// the real domain's instead of guessing what "normal" looks like for it.
	//
	// Capping happens before WithOriginal, not after, so the seed's own row —
	// prepended unconditionally — can never be truncated away by --limit.
	//
	// TLD swap is exempt from the per-technique cap: its candidate count is
	// exactly the TLD list the user chose, and capping it would silently cut
	// a `--tld-profile full` sweep to the first ~500 TLDs alphabetically.
	// The explicit --limit still bounds it.
	permuted := engine.Cap(engine.Permute(seed, techniques), engine.DefaultTechniqueCap, opts.Limit, engine.TechniqueTLD)
	candidates := engine.WithOriginal(seed, permuted)

	findings, err := resolveAll(ctx, opts, candidates)
	result := Result{Seed: seed, Findings: findings}
	if err != nil {
		// A cancelled scan still returns what it managed to resolve, but it is
		// never persisted: a partial run recorded as a scan would make every
		// candidate it never reached look like it had gone away.
		return result, err
	}

	if opts.Store == nil {
		return result, nil
	}
	return persist(ctx, opts, seed.String(), result)
}

// resolveAll resolves every candidate concurrently and returns the findings in
// candidate order.
//
// A candidate that fails to resolve is reported with its error rather than
// dropped, and does not stop the run: one broken name in a thousand is an
// ordinary outcome of bulk DNS, and aborting on it would throw away the other
// nine hundred and ninety-nine. Only the context ending stops the scan.
func resolveAll(ctx context.Context, opts Options, candidates []engine.Candidate) ([]Finding, error) {
	dnsResolver := resolver.RateLimited(opts.Resolver, opts.QPS)
	detector := wildcard.New(dnsResolver)

	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}

	findings := make([]Finding, len(candidates))
	resolved := make([]bool, len(candidates))

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(concurrency)
	for i, candidate := range candidates {
		group.Go(func() error {
			finding, err := resolveOne(groupCtx, dnsResolver, detector, candidate)
			if err != nil {
				return err
			}
			// Distinct indices, so no two workers ever touch the same element.
			findings[i] = finding
			resolved[i] = true
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return compact(findings, resolved), err
	}
	return findings, nil
}

// resolveOne resolves a single candidate. It returns an error only when the
// context ended; anything else is recorded on the finding.
func resolveOne(ctx context.Context, r resolver.Resolver, detector *wildcard.Detector, candidate engine.Candidate) (Finding, error) {
	finding := Finding{
		Candidate:   candidate.Domain,
		Registrable: candidate.Registrable,
		Technique:   candidate.Technique,
	}

	signals, err := resolver.Signals(ctx, r, candidate.Domain, candidate.Registrable)
	if err != nil {
		if ctx.Err() != nil {
			return Finding{}, ctx.Err()
		}
		finding.Error = err.Error()
	}
	finding.Registered = signals.Registered
	finding.HasNS = signals.HasNS
	finding.HasA = signals.HasA
	finding.HasMX = signals.HasMX
	finding.Addresses = sorted(signals.Addresses)
	finding.NS = sorted(signals.NS)
	finding.MX = sorted(signals.MX)
	finding.Rcode = signals.Rcode

	// The zone is probed at most once per scan however many candidates sit under
	// it, and a zone that cannot be probed is simply not subtracted — failing to
	// characterise a zone is no reason to fail the candidates in it.
	signature, err := detector.Signature(ctx, candidate.Suffix)
	if err != nil {
		if ctx.Err() != nil {
			return Finding{}, ctx.Err()
		}
		return finding, nil
	}
	finding.Wildcard = !signature.IsReal(finding.Addresses)
	return finding, nil
}

// compact drops the slots no worker got to, so a cancelled scan reports the
// candidates it resolved rather than a run of blank rows.
func compact(findings []Finding, resolved []bool) []Finding {
	out := make([]Finding, 0, len(findings))
	for i, finding := range findings {
		if resolved[i] {
			out = append(out, finding)
		}
	}
	return out
}

// sorted returns a sorted copy, so a resolver that round-robins its answers
// cannot make two identical scans render differently.
func sorted(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := make([]string, len(values))
	copy(out, values)
	sort.Strings(out)
	return out
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

	scanID, err := opts.Store.RecordScan(ctx, seed, opts.Profile, toStore(result.Findings))
	if err != nil {
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
