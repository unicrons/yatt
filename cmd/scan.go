package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/andoniaf/yatt/internal/config"
	"github.com/andoniaf/yatt/internal/render"
	"github.com/andoniaf/yatt/internal/resolver"
	"github.com/andoniaf/yatt/internal/scan"
	"github.com/andoniaf/yatt/internal/triage"
	"github.com/andoniaf/yatt/pkg/engine"
)

// newResolver is the seam tests replace with a scripted resolver, so the scan
// command can be exercised end to end without touching the network.
var newResolver = func(addr string, timeout time.Duration) (resolver.Resolver, error) {
	return resolver.New(addr, timeout)
}

// scanOptions holds the flags local to the scan command.
type scanOptions struct {
	status           []string
	excludeStatus    []string
	technique        []string
	tldProfile       string
	tldFile          string
	limit            int
	showUnregistered bool
	profile          string
	wide             bool
}

// triageFilter resolves the triage flags, rejecting the run if any status is
// unknown. Validating before resolving anything means a typo costs an error
// message rather than a full scan whose output is then silently over-filtered.
func (o *scanOptions) triageFilter() (scan.TriageFilter, error) {
	include, err := triage.ParseAll(o.status)
	if err != nil {
		return scan.TriageFilter{}, err
	}
	exclude, err := triage.ParseAll(o.excludeStatus)
	if err != nil {
		return scan.TriageFilter{}, err
	}
	return scan.TriageFilter{Include: include, Exclude: exclude}, nil
}

// techniques resolves --technique to the concrete implementations, defaulting
// to every registered technique when the flag was not given.
func (o *scanOptions) techniques() ([]engine.Technique, error) {
	if len(o.technique) == 0 {
		return engine.All(), nil
	}
	return engine.Select(o.technique)
}

// tlds reads --tld-file, when set. A custom list always wins over
// --tld-profile — resolved further down in scan.Run, once the seed's own
// suffix is known — so this only needs to answer "was a file given".
func (o *scanOptions) tlds() ([]string, error) {
	if o.tldFile == "" {
		return nil, nil
	}
	f, err := os.Open(o.tldFile)
	if err != nil {
		return nil, fmt.Errorf("--tld-file: %w", err)
	}
	defer func() { _ = f.Close() }()

	tlds, err := engine.ParseTLDList(f)
	if err != nil {
		return nil, fmt.Errorf("--tld-file: %w", err)
	}
	if len(tlds) == 0 {
		// An explicitly given file that yields nothing is an error, not a
		// fallback: scan.Run treats an empty list as "no custom list" and
		// would silently sweep the --tld-profile the file was meant to
		// replace.
		return nil, fmt.Errorf("--tld-file %s: no TLDs found (blank and # lines are ignored)", o.tldFile)
	}
	return tlds, nil
}

func newScanCmd(global *globalOptions) *cobra.Command {
	opts := &scanOptions{}

	cmd := &cobra.Command{
		Use:   "scan <domain>",
		Short: "Generate look-alike domains for a seed and resolve them",
		Long: "scan permutes the seed domain's second-level label, resolves every candidate,\n" +
			"and reports whether it is registered along with its NS, MX and A record presence.\n\n" +
			"The seed itself is the first row, marked `original` and resolved like any other,\n" +
			"so the candidates can be read against the real domain's signals. It is recorded\n" +
			"and diffed like a candidate but is never hidden by --status/--exclude-status.\n\n" +
			"Registration is decided by the response code of an NS query at the candidate's\n" +
			"registrable domain, so parked and MX-only domains are still reported as registered.\n\n" +
			"Each zone is probed once for a catch-all: a candidate whose addresses match what\n" +
			"its zone hands out for names that do not exist is marked in the wildcard column,\n" +
			"since such a zone answers for every look-alike whether or not anyone registered it.\n\n" +
			"Every scan is recorded, and each candidate is reported as new, changed, unchanged\n" +
			"or gone relative to the previous scan of the same seed, alongside the standing\n" +
			"triage verdict carried over from `yatt triage`.\n\n" +
			"Only registered candidates are reported by default, listed before the rest, and\n" +
			"--show-unregistered adds back the ones nobody has taken. A candidate whose\n" +
			"resolution failed is always reported: nothing answered for it, which is not the\n" +
			"same as it being unregistered.\n\n" +
			"Filtering applies to the report only: every candidate is still resolved and\n" +
			"recorded, so a filtered scan does not leave gaps in the seed's history.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScan(cmd, global, opts, args[0])
		},
	}

	flags := cmd.Flags()
	flags.StringSliceVar(&opts.status, "status", nil,
		"report only candidates with these triage statuses ("+triage.List()+")")
	flags.StringSliceVar(&opts.excludeStatus, "exclude-status", nil,
		"report every candidate except those with these triage statuses")
	flags.StringSliceVar(&opts.technique, "technique", nil,
		"techniques to run, comma-separated ("+strings.Join(engine.Names(), ", ")+") (default: all)")
	flags.StringVar(&opts.tldProfile, "tld-profile", engine.TLDProfileCommon,
		fmt.Sprintf("TLD list the tld technique swaps against (%s|%s)", engine.TLDProfileCommon, engine.TLDProfileFull))
	flags.StringVar(&opts.tldFile, "tld-file", "",
		"custom TLD list, one per line; overrides --tld-profile")
	flags.IntVar(&opts.limit, "limit", 0,
		"cap the total candidate count after per-technique capping (0 for unlimited)")
	flags.BoolVar(&opts.showUnregistered, "show-unregistered", false,
		"also report candidates nobody has registered")
	flags.BoolVar(&opts.wide, "wide", false,
		"add the enrichment-link column to the table (always present in json/ndjson output)")
	flags.StringVar(&opts.profile, "profile", "",
		"scan profile ("+strings.Join(config.Names(), ", ")+", or one defined in --config); "+
			"sets technique/tld-profile/concurrency/qps/timeout/limit, each still overridable by its own flag")

	return cmd
}

// progressReporter returns a live progress reporter when stderr is actually a
// terminal, and nil otherwise: the reporter's carriage-return rewrites are
// for a human watching, and in a pipe or a CI log they are just noise. This
// keeps the standing contract that stdout is machine-readable and stderr
// carries the chatter, with redirection as the off switch.
func progressReporter(stderr io.Writer) *render.Progress {
	f, ok := stderr.(*os.File)
	if !ok {
		return nil
	}
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return nil
	}
	return render.NewProgress(f)
}

// profileFlags maps a Profile field's own name (config.Profile's
// mapstructure tag, also what a config file author writes) to the flag that
// overrides it, so applyProfile can tell a preset from a choice the user
// actually typed.
var profileFlags = map[string]string{
	"techniques":  "technique",
	"tld_profile": "tld-profile",
	"concurrency": "concurrency",
	"qps":         "qps",
	"timeout":     "timeout",
	"limit":       "limit",
}

// applyProfile overlays a resolved profile onto the flags governing a scan:
// every field the profile sets is applied, except one the corresponding flag
// was actually given — that flag's value stands regardless of what the
// profile says, so naming a profile is never a way to fight a flag typed
// right next to it.
func applyProfile(cmd *cobra.Command, global *globalOptions, opts *scanOptions, profile config.Profile) {
	flags := cmd.Flags()
	profile = profile.Overlay(func(field string) bool {
		return flags.Changed(profileFlags[field])
	})

	if len(profile.Techniques) > 0 {
		opts.technique = profile.Techniques
	}
	if profile.TLDProfile != "" {
		opts.tldProfile = profile.TLDProfile
	}
	if profile.Limit != 0 {
		opts.limit = profile.Limit
	}
	if profile.Concurrency != 0 {
		global.concurrency = profile.Concurrency
	}
	if profile.QPS != 0 {
		global.qps = profile.QPS
	}
	if profile.Timeout != 0 {
		global.timeout = profile.Timeout
	}
}

func runScan(cmd *cobra.Command, global *globalOptions, opts *scanOptions, seed string) error {
	renderOpts := global.renderOptions()
	if opts.wide {
		renderOpts = append(renderOpts, render.Wide())
	}
	renderer, err := render.New(global.output, renderOpts...)
	if err != nil {
		return err
	}

	// The profile is resolved and applied before anything below reads
	// opts/global, so every flag it can set — technique, tld-profile,
	// concurrency, qps, timeout, limit — sees the profile's value unless the
	// matching flag was itself given. ProfileName already resolves --profile
	// over YATT_PROFILE over a top-level "profile" key in the config file,
	// since --profile was bound to the same Config in root.go.
	profileName := global.config.ProfileName()
	if profileName != "" {
		profile, err := global.config.Resolve(profileName)
		if err != nil {
			return err
		}
		applyProfile(cmd, global, opts, profile)
	}

	filter, err := opts.triageFilter()
	if err != nil {
		return err
	}

	techniques, err := opts.techniques()
	if err != nil {
		return err
	}

	tlds, err := opts.tlds()
	if err != nil {
		return err
	}

	dnsResolver, err := newResolver(global.resolver, global.timeout)
	if err != nil {
		return err
	}

	scanStore, err := global.openStore()
	if err != nil {
		return err
	}
	defer func() { _ = scanStore.Close() }()

	if global.verbose {
		// Progress output is best-effort: a failed write to stderr must not
		// abort a scan.
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "scanning %s\n", seed)
	}

	scanOpts := scan.Options{
		Seed:        seed,
		Resolver:    dnsResolver,
		Techniques:  techniques,
		Store:       scanStore,
		Profile:     profileName,
		Concurrency: global.concurrency,
		QPS:         global.qps,
		TLDProfile:  opts.tldProfile,
		TLDs:        tlds,
		Limit:       opts.limit,
	}

	// The live progress line only makes sense on a terminal: its carriage
	// returns would be junk in a log or a pipe, so anything that is not a
	// character device suppresses it — no flag needed, redirecting stderr is
	// the switch. Stdout stays machine-readable either way.
	progress := progressReporter(cmd.ErrOrStderr())
	if progress != nil {
		scanOpts.Progress = progress.Update
	}

	result, err := scan.Run(cmd.Context(), scanOpts)
	if progress != nil {
		// Cleared before anything else prints — the table on success, the
		// error on failure — so neither lands on top of the bar's remains.
		progress.Finish()
	}
	if err != nil {
		return err
	}

	// Candidates that disappeared since the previous scan are reported
	// alongside the current ones; a look-alike domain lapsing is a finding, not
	// an absence.
	findings := make([]scan.Finding, 0, len(result.Findings)+len(result.Gone))
	findings = append(findings, result.Findings...)
	findings = append(findings, result.Gone...)

	// Filtering happens after persistence, never before it: the scan record must
	// describe what was actually resolved, or the next run's diff would report
	// every filtered-out candidate as gone and then as new again.
	reported := filter.Apply(findings)
	triaged := len(findings) - len(reported)

	// Registered candidates lead the report whether or not the unregistered
	// ones are along for the ride, so the rows worth acting on are the rows
	// read first.
	unregistered := 0
	if !opts.showUnregistered {
		before := len(reported)
		reported = scan.HideUnregistered(reported)
		unregistered = before - len(reported)
	}
	reported = scan.RegisteredFirst(reported)

	if err := renderer.Render(cmd.OutOrStdout(), reported); err != nil {
		return err
	}

	if global.verbose {
		summary := render.Summarize(reported)
		if triaged > 0 {
			summary += fmt.Sprintf(" (%d hidden by --status/--exclude-status)", triaged)
		}
		if unregistered > 0 {
			summary += fmt.Sprintf(" (%d unregistered hidden; --show-unregistered to see them)", unregistered)
		}
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), summary)
	}
	return nil
}
