// Package cmd wires the yatt command tree.
package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/andoniaf/yatt/internal/config"
	"github.com/andoniaf/yatt/internal/render"
	"github.com/andoniaf/yatt/internal/resolver"
	"github.com/andoniaf/yatt/internal/scan"
	"github.com/andoniaf/yatt/internal/store"
	"github.com/andoniaf/yatt/internal/store/remote"
)

// newStore is the seam tests replace so a command test never touches the user's
// real database.
var newStore = func(path string) (store.Store, error) {
	return store.Open(path)
}

// newRemoteClient is the seam tests replace with an in-memory S3 fake, so a
// command test never talks to a real endpoint.
var newRemoteClient = remote.NewDefaultClient

// globalOptions holds the flags declared on the root command and inherited by
// every subcommand.
type globalOptions struct {
	output      string
	resolver    string
	timeout     time.Duration
	concurrency int
	qps         float64
	db          string
	verbose     bool
	punycode    bool
	configPath  string
	// config is resolved once per invocation, in PersistentPreRunE, once every
	// flag the command was actually given is known — a scan profile is applied
	// against that, not against the flags as declared.
	config *config.Config
}

// renderOptions returns the table options every findings-rendering command
// inherits from the global flags, so `scan` and `diff` cannot drift on how a
// candidate is displayed.
func (o *globalOptions) renderOptions() []render.Option {
	var opts []render.Option
	if o.punycode {
		opts = append(opts, render.Punycode())
	}
	return opts
}

// openStore opens the scan database, defaulting to a per-user location when
// --db was not given. An s3:// value routes to the remote backend, which holds
// a lock on the remote database for the duration of the command; the command
// is needed to name the operation in that lock and to carry the context.
func (o *globalOptions) openStore(cmd *cobra.Command) (store.Store, error) {
	path := o.db
	if remote.IsRemote(path) {
		client, err := newRemoteClient(cmd.Context())
		if err != nil {
			return nil, err
		}
		return remote.Open(cmd.Context(), client, path, operation(cmd))
	}
	if path == "" {
		var err error
		if path, err = store.DefaultPath(); err != nil {
			return nil, fmt.Errorf("cannot determine the default database location, pass --db: %w", err)
		}
	}
	return newStore(path)
}

// operation names the running command for the lock metadata: the command path
// without the binary name, so a subcommand reads as "triage list" rather than
// an ambiguous "list".
func operation(cmd *cobra.Command) string {
	return strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" ")
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
	flags.IntVar(&opts.concurrency, "concurrency", scan.DefaultConcurrency,
		"how many candidates to resolve at once")
	flags.Float64Var(&opts.qps, "qps", resolver.DefaultQPS,
		"cap DNS queries per second across all workers (0 for unlimited)")
	flags.StringVar(&opts.db, "db", "",
		"scan database path, or s3://bucket/key to keep it in S3\n"+
			"(default: yatt/yatt.db under the user config directory)")
	flags.BoolVarP(&opts.verbose, "verbose", "v", false, "log scan progress to stderr")
	flags.BoolVar(&opts.punycode, "punycode", false,
		"show IDN candidates in their punycode (xn--) form instead of Unicode")
	flags.StringVar(&opts.configPath, "config", "",
		"config file (YAML/JSON/TOML) defining scan profiles (default: none)")

	// Resolving the config happens once, after Cobra has parsed every flag the
	// invoked command was given: a bound flag only outranks the config file and
	// YATT_-prefixed environment variables once it can be asked whether the
	// user actually set it, and that answer only exists post-parse.
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		cfg := config.New()
		if err := cfg.BindPFlags(cmd.Flags()); err != nil {
			return err
		}
		if err := cfg.Load(opts.configPath); err != nil {
			return err
		}
		opts.config = cfg
		return nil
	}

	root.AddCommand(newScanCmd(opts))
	root.AddCommand(newHistoryCmd(opts))
	root.AddCommand(newDiffCmd(opts))
	root.AddCommand(newTriageCmd(opts))
	root.AddCommand(newStateCmd(opts))

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
