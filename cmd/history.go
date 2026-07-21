package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/andoniaf/yatt/internal/render"
	"github.com/andoniaf/yatt/internal/store"
)

func newHistoryCmd(global *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "history <domain>",
		Short: "List the recorded scans of a seed domain",
		Long: "history lists every scan recorded for a seed, most recent first, with the\n" +
			"number of candidates each one produced and how many of them were registered.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHistory(cmd, global, args[0])
		},
	}
}

func runHistory(cmd *cobra.Command, global *globalOptions, seed string) error {
	renderer, err := render.New(global.output)
	if err != nil {
		return err
	}

	scanStore, err := global.openStore(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = scanStore.Close() }()

	seed = store.NormalizeSeed(seed)
	scans, err := scanStore.ListScans(cmd.Context(), seed)
	if err != nil {
		return err
	}

	if err := renderer.RenderScans(cmd.OutOrStdout(), scans); err != nil {
		return err
	}

	// An unscanned seed is not an error, but silence would read like a bug, so
	// the hint goes to stderr where it cannot corrupt piped output.
	if len(scans) == 0 {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "no recorded scans for %s: run `yatt scan %s` first\n", seed, seed)
	}
	return nil
}
