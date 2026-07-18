package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/andoniaf/yatt/internal/render"
	"github.com/andoniaf/yatt/internal/resolver"
	"github.com/andoniaf/yatt/internal/scan"
	"github.com/andoniaf/yatt/internal/triage"
)

// newResolver is the seam tests replace with a scripted resolver, so the scan
// command can be exercised end to end without touching the network.
var newResolver = func(addr string, timeout time.Duration) (resolver.Resolver, error) {
	return resolver.New(addr, timeout)
}

// scanOptions holds the flags local to the scan command.
type scanOptions struct {
	status        []string
	excludeStatus []string
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
			"Every scan is recorded, and each candidate is reported as new, changed, unchanged\n" +
			"or gone relative to the previous scan of the same seed, alongside the standing\n" +
			"triage verdict carried over from `yatt triage`.\n\n" +
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

	return cmd
}

func runScan(cmd *cobra.Command, global *globalOptions, opts *scanOptions, seed string) error {
	renderer, err := render.New(global.output)
	if err != nil {
		return err
	}

	filter, err := opts.triageFilter()
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

	result, err := scan.Run(cmd.Context(), scan.Options{
		Seed:     seed,
		Resolver: dnsResolver,
		Store:    scanStore,
	})
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
	if err := renderer.Render(cmd.OutOrStdout(), reported); err != nil {
		return err
	}

	if global.verbose {
		summary := render.Summarize(reported)
		if hidden := len(findings) - len(reported); hidden > 0 {
			summary += fmt.Sprintf(" (%d hidden by --status/--exclude-status)", hidden)
		}
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), summary)
	}
	return nil
}
