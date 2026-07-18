package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/andoniaf/yatt/internal/render"
	"github.com/andoniaf/yatt/internal/resolver"
	"github.com/andoniaf/yatt/internal/scan"
)

// newResolver is the seam tests replace with a scripted resolver, so the scan
// command can be exercised end to end without touching the network.
var newResolver = func(addr string, timeout time.Duration) (resolver.Resolver, error) {
	return resolver.New(addr, timeout)
}

func newScanCmd(global *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "scan <domain>",
		Short: "Generate look-alike domains for a seed and resolve them",
		Long: "scan permutes the seed domain's second-level label, resolves every candidate,\n" +
			"and reports whether it is registered along with its NS, MX and A record presence.\n\n" +
			"Registration is decided by the response code of an NS query at the candidate's\n" +
			"registrable domain, so parked and MX-only domains are still reported as registered.\n\n" +
			"Every scan is recorded, and each candidate is reported as new, changed, unchanged\n" +
			"or gone relative to the previous scan of the same seed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScan(cmd, global, args[0])
		},
	}
}

func runScan(cmd *cobra.Command, global *globalOptions, seed string) error {
	renderer, err := render.New(global.output)
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
	if err := renderer.Render(cmd.OutOrStdout(), findings); err != nil {
		return err
	}

	if global.verbose {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), render.Summarize(findings))
	}
	return nil
}
