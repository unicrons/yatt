// Package cmd wires the yatt command tree.
package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/andoniaf/yatt/internal/render"
)

// globalOptions holds the flags declared on the root command and inherited by
// every subcommand.
type globalOptions struct {
	output   string
	resolver string
	timeout  time.Duration
	verbose  bool
}

// NewRootCmd builds the command tree.
//
// Commands are built by a constructor rather than registered onto a package
// global, so each test gets an independent tree with its own flag state.
func NewRootCmd() *cobra.Command {
	opts := &globalOptions{}

	root := &cobra.Command{
		Use:   "yatt",
		Short: "Yet Another Typosquatting Tool",
		Long: "yatt generates look-alike variants of a domain, resolves them over DNS,\n" +
			"and reports which ones are registered.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	flags := root.PersistentFlags()
	flags.StringVarP(&opts.output, "output", "o", "table",
		fmt.Sprintf("output format (%s)", strings.Join(render.Formats, "|")))
	flags.StringVar(&opts.resolver, "resolver", "",
		"upstream DNS resolver as host[:port] (default: the system resolver)")
	flags.DurationVar(&opts.timeout, "timeout", 3*time.Second, "per-query DNS timeout")
	flags.BoolVarP(&opts.verbose, "verbose", "v", false, "log scan progress to stderr")

	root.AddCommand(newScanCmd(opts))

	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	root := NewRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}
