package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/andoniaf/yatt/internal/render"
	"github.com/andoniaf/yatt/internal/scan"
	"github.com/andoniaf/yatt/internal/store"
)

func newDiffCmd(global *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "diff <domain>",
		Short: "Compare a seed's last two recorded scans",
		Long: "diff reports what changed between the two most recent recorded scans of a seed:\n" +
			"candidates that appeared, candidates whose signals moved, and candidates that are\n" +
			"gone. Nothing is resolved — the comparison reads only what previous scans stored.\n\n" +
			"A candidate counts as changed when its registered, NS, MX or A signal moved.\n" +
			"Rotating addresses on an otherwise unchanged candidate are not a change.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd, global, args[0])
		},
	}
}

func runDiff(cmd *cobra.Command, global *globalOptions, seed string) (retErr error) {
	renderer, err := render.New(global.output, global.renderOptions()...)
	if err != nil {
		return err
	}

	scanStore, err := global.openStore(cmd)
	if err != nil {
		return err
	}
	// Close is part of the command's outcome, not cleanup: for a remote
	// database it performs the upload and lock release, and swallowing its
	// error would report success for a run whose data never left the machine.
	defer func() { retErr = errors.Join(retErr, scanStore.Close()) }()

	comparison, err := scan.CompareLast(cmd.Context(), scanStore, store.NormalizeSeed(seed))
	if err != nil {
		return err
	}

	changes := scan.Changes(comparison.Result)
	if err := renderer.Render(cmd.OutOrStdout(), changes); err != nil {
		return err
	}

	stderr := cmd.ErrOrStderr()
	switch {
	case comparison.Previous == nil:
		_, _ = fmt.Fprintf(stderr, "scan %d is the first recorded scan of %s: every candidate is new\n",
			comparison.Current.ID, comparison.Current.Seed)
	case !scan.HasChanges(comparison.Result):
		_, _ = fmt.Fprintf(stderr, "no changes between scans %d and %d\n",
			comparison.Previous.ID, comparison.Current.ID)
	case global.verbose:
		_, _ = fmt.Fprintf(stderr, "scan %d vs %d: %s\n",
			comparison.Previous.ID, comparison.Current.ID, render.Summarize(changes))
	}
	return nil
}
