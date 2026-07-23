package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version, Commit, and Date are set via -ldflags at build time by GoReleaser.
// They default to placeholders so a plain `go build` still reports something
// sane; resolveVersion recovers a real version for `go install` builds too.
var (
	Version = "devel"
	Commit  = "none"
	Date    = "unknown"
)

// resolveVersion returns the version to report. A GoReleaser build injects
// Version directly; a `go install` build leaves the placeholder but records the
// module version in the build info, so fall back to that before giving up.
func resolveVersion() string {
	if Version != "devel" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "devel"
}

// NewVersionCmd builds the "version" subcommand.
func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "yatt %s (commit %s, built %s)\n", resolveVersion(), Commit, Date)
			return err
		},
	}
}
