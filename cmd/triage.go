package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/andoniaf/yatt/internal/render"
	"github.com/andoniaf/yatt/internal/store"
	"github.com/andoniaf/yatt/internal/triage"
)

// triageOptions holds the flags local to the triage command.
type triageOptions struct {
	status string
	note   string
	force  bool
}

func newTriageCmd(global *globalOptions) *cobra.Command {
	opts := &triageOptions{}

	cmd := &cobra.Command{
		Use:   "triage <domain> <candidate>",
		Short: "Record a verdict on a candidate look-alike domain",
		Long: "triage records an analyst's standing verdict on one candidate of one seed.\n\n" +
			"The verdict is keyed by seed and candidate rather than by scan, so it survives\n" +
			"every future scan: a candidate marked owned or benign stays marked, and the\n" +
			"backlog of untriaged candidates shrinks instead of resetting on every run.\n\n" +
			"Passing --note without --status amends the note and leaves the verdict alone.\n\n" +
			"The candidate must have appeared in a recorded scan of the seed, otherwise the\n" +
			"verdict would be filed where no scan can ever read it back. Use --force to record\n" +
			"one anyway.\n\n" +
			"Valid statuses: " + triage.ListSettable(),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTriage(cmd, global, opts, args[0], args[1])
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.status, "status", "", "verdict to record ("+triage.ListSettable()+")")
	flags.StringVar(&opts.note, "note", "", "free-text note recorded alongside the verdict")
	flags.BoolVar(&opts.force, "force", false,
		"record the verdict even if the candidate has never appeared in a scan of this seed\n"+
			"(needed to pre-record a verdict before the seed's first scan, or to judge a candidate\n"+
			"the techniques in use did not produce; the verdict is stored either way, but stays\n"+
			"invisible until some scan surfaces the candidate)")

	cmd.AddCommand(newTriageListCmd(global))
	return cmd
}

func runTriage(cmd *cobra.Command, global *globalOptions, opts *triageOptions, seed, candidate string) (retErr error) {
	renderer, err := render.New(global.output)
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

	seed = store.NormalizeSeed(seed)
	candidate = store.NormalizeCandidate(candidate)

	// The pair is checked before anything else about the command, because if the
	// pair is wrong nothing else about the command matters: a verdict filed under
	// a seed the candidate cannot appear under is write-only data that reports as
	// a successful triage and then never shows up again.
	if !opts.force {
		if err := checkPair(cmd.Context(), scanStore, seed, candidate); err != nil {
			return err
		}
	}

	// The existing verdict is read first for two reasons: the transition rules
	// are stated in terms of it, and an amendment that only supplies one of
	// --status/--note must carry the other forward rather than blank it.
	existing, err := scanStore.GetTriage(cmd.Context(), seed)
	if err != nil {
		return err
	}
	current, triaged := existing[candidate]

	status, err := resolveStatus(cmd, opts, current.Status, triaged)
	if err != nil {
		return err
	}
	if err := triage.CanTransition(current.Status, status); err != nil {
		return err
	}

	note := current.Note
	if cmd.Flags().Changed("note") {
		note = opts.note
	}

	stored, err := scanStore.SetTriage(cmd.Context(), seed, candidate, status, note)
	if err != nil {
		return err
	}

	return renderer.RenderTriage(cmd.OutOrStdout(), []store.Triage{stored})
}

// checkPair refuses a (seed, candidate) pair no scan has ever produced.
//
// The failure this prevents is silent rather than loud: `yatt triage
// example.com nicrons.cloud` for a candidate of unicrons.cloud stores happily,
// prints the same confirmation a correct one does, and is then invisible
// forever, because every reader of that verdict looks it up under the seed that
// can actually surface the candidate. The check reads recorded history rather
// than re-permuting the seed, so a candidate a previous run produced still
// counts even when today's technique flags would not emit it.
func checkPair(ctx context.Context, scanStore store.Store, seed, candidate string) error {
	origin, err := scanStore.LookupCandidate(ctx, seed, candidate, 0)
	if err != nil {
		return err
	}
	// Each branch below leads with the most actionable thing known, because the
	// first clause is the one that gets read. Where another seed did record the
	// candidate, that is always it: it converts "this failed" into the command
	// the user meant to type.
	switch {
	case origin.Recorded:
		return nil

	// A seed with no history at all is not the candidate's fault, and blaming
	// the candidate would send the user hunting for a typo that is not there.
	// It is stated first because it is the more fundamental problem: there is
	// nothing to check the candidate against.
	case !origin.SeedScanned && len(origin.OtherSeeds) == 0:
		return fmt.Errorf("no recorded scans for %s: run `yatt scan %s` first, or pass --force to record the verdict now",
			seed, seed)

	// A mistyped seed looks exactly like an unscanned one, so the suggestion
	// still rides along — it is usually the actual explanation.
	case !origin.SeedScanned:
		return fmt.Errorf("no recorded scans for %s: run `yatt scan %s` first — or did you mean `yatt triage %s %s`? %s is recorded under %s (pass --force to record it under %s anyway)",
			seed, seed, origin.OtherSeeds[0], candidate,
			candidate, strings.Join(origin.OtherSeeds, ", "), seed)

	case len(origin.OtherSeeds) > 0:
		return fmt.Errorf("%s has never appeared in a scan of %s, but it is recorded under %s: did you mean `yatt triage %s %s`? (pass --force to record it under %s anyway)",
			candidate, seed, strings.Join(origin.OtherSeeds, ", "),
			origin.OtherSeeds[0], candidate, seed)

	default:
		return fmt.Errorf("%s has never appeared in a scan of %s: check the spelling, or pass --force to record the verdict anyway",
			candidate, seed)
	}
}

// resolveStatus decides which verdict to store.
//
// An explicit --status always wins. Without one, an existing verdict is kept —
// that is what makes `--note` alone a note amendment — and a candidate with no
// verdict at all is an error rather than a silent default, since guessing an
// analyst's judgement is the one thing this command must not do.
func resolveStatus(cmd *cobra.Command, opts *triageOptions, current triage.Status, triaged bool) (triage.Status, error) {
	if cmd.Flags().Changed("status") {
		return triage.Parse(opts.status)
	}
	if triaged {
		return current, nil
	}
	return "", fmt.Errorf("no verdict recorded yet: pass --status (valid: %s)", triage.ListSettable())
}

func newTriageListCmd(global *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list <domain>",
		Short: "List every recorded verdict for a seed domain",
		Long: "list shows the verdicts recorded against a seed's candidates, most recently\n" +
			"updated first. Candidates nobody has judged are absent: they are reported as\n" +
			"new by `yatt scan`, which is where the untriaged backlog belongs.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTriageList(cmd, global, args[0])
		},
	}
}

func runTriageList(cmd *cobra.Command, global *globalOptions, seed string) (retErr error) {
	renderer, err := render.New(global.output)
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

	seed = store.NormalizeSeed(seed)
	entries, err := scanStore.ListTriage(cmd.Context(), seed)
	if err != nil {
		return err
	}

	if err := renderer.RenderTriage(cmd.OutOrStdout(), entries); err != nil {
		return err
	}

	// Nothing triaged yet is an ordinary state, not an error, but silence would
	// read like a bug. The hint goes to stderr where it cannot corrupt a pipe.
	if len(entries) == 0 {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "no verdicts recorded for %s: run `yatt triage %s <candidate> --status ...` first\n", seed, seed)
	}
	return nil
}
