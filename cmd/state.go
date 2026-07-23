package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/unicrons/yatt/internal/store"
	"github.com/unicrons/yatt/internal/store/remote"
)

func newStateCmd(global *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Manage the remote state database",
		Long: "state manages a database kept in S3 (--db s3://bucket/key): migrating a local\n" +
			"database there, taking a local copy, and clearing the lock a crashed process\n" +
			"left behind. Every subcommand requires --db to name a remote database.",
	}
	cmd.AddCommand(newStateUnlockCmd(global))
	cmd.AddCommand(newStatePushCmd(global))
	cmd.AddCommand(newStatePullCmd(global))
	return cmd
}

// remoteURL is the guard every state subcommand shares: these commands only
// mean something against a remote database, and silently operating on a local
// path would be a no-op that reports success.
func (o *globalOptions) remoteURL(name string) (string, error) {
	if !remote.IsRemote(o.db) {
		return "", fmt.Errorf("state %s only applies to a remote database: pass --db s3://bucket/key (got %q)", name, o.db)
	}
	return o.db, nil
}

func newStateUnlockCmd(global *globalOptions) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "unlock",
		Short: "Force-clear the lock on the remote state database",
		Long: "unlock clears the lock a crashed yatt process left on the remote database.\n\n" +
			"Only clear a lock whose holder is actually gone: clearing a live one lets two\n" +
			"processes write concurrently, and whichever uploads second will fail its\n" +
			"conditional write and lose its run's changes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, err := global.remoteURL("unlock")
			if err != nil {
				return err
			}
			client, err := newRemoteClient(cmd.Context(), url)
			if err != nil {
				return err
			}

			info, err := remote.ReadLock(cmd.Context(), client, url)
			if errors.Is(err, remote.ErrNoLock) {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "no lock is held on %s\n", url)
				return nil
			}
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "lock %s\n", info.Describe())
			if !force {
				ok, err := confirm(cmd, "Clear this lock? Only do this if that process is gone.")
				if err != nil {
					return err
				}
				if !ok {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "lock left in place")
					return nil
				}
			}

			// The lock shown above is the one Unlock clears: it refuses if the
			// lock changed hands while the user deliberated, so a live holder
			// that replaced the stale one cannot be cleared unseen.
			if err := remote.Unlock(cmd.Context(), client, url, info); err != nil {
				// The holder finishing on its own between the prompt and the
				// delete is the desired end state, not a failure.
				if errors.Is(err, remote.ErrNoLock) {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "lock on %s already released\n", url)
					return nil
				}
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "lock on %s cleared\n", url)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "clear the lock without asking for confirmation")
	return cmd
}

func newStatePushCmd(global *globalOptions) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "push [local-db]",
		Short: "Upload a local state database to the remote location",
		Long: "push uploads a local database file as the remote one — the migration path from\n" +
			"a local-only setup. Without an argument it pushes the default local database.\n\n" +
			"It refuses to replace a database that already exists remotely; pass --force to\n" +
			"replace it anyway, for instance to restore the copy a failed upload preserved.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url, err := global.remoteURL("push")
			if err != nil {
				return err
			}
			localPath := ""
			if len(args) == 1 {
				localPath = args[0]
			} else if localPath, err = store.DefaultPath(); err != nil {
				return fmt.Errorf("cannot determine the default database location, name one explicitly: %w", err)
			}
			client, err := newRemoteClient(cmd.Context(), url)
			if err != nil {
				return err
			}

			if err := remote.Push(cmd.Context(), client, localPath, url, force); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "pushed %s to %s\n", localPath, url)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "replace the remote database if one already exists")
	return cmd
}

func newStatePullCmd(global *globalOptions) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "pull <local-path>",
		Short: "Download the remote state database to a local file",
		Long: "pull downloads a copy of the remote database — a consistent snapshot, since a\n" +
			"single S3 read is atomic. The destination is explicit rather than defaulted, so\n" +
			"a pull can never land on a local database by surprise.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url, err := global.remoteURL("pull")
			if err != nil {
				return err
			}
			client, err := newRemoteClient(cmd.Context(), url)
			if err != nil {
				return err
			}

			if err := remote.Pull(cmd.Context(), client, url, args[0], force); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "pulled %s to %s\n", url, args[0])
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite the local file if it already exists")
	return cmd
}

// confirm asks a yes/no question on the command's own streams, so tests can
// script the answer and captured output stays coherent.
func confirm(cmd *cobra.Command, question string) (bool, error) {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N]: ", question)
	answer, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && answer == "" {
		return false, fmt.Errorf("reading confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}
