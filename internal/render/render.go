// Package render turns findings into the output formats the CLI offers.
//
// Renderers write to an io.Writer rather than os.Stdout so commands can render
// through cmd.OutOrStdout() and tests can render into a bytes.Buffer.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/andoniaf/yatt/internal/scan"
	"github.com/andoniaf/yatt/internal/store"
)

// Formats lists every supported --output value.
var Formats = []string{"table", "json", "ndjson"}

// Renderer writes findings in one output format.
//
// Every implementation emits the seed's own row first, whatever order the
// findings arrive in. Enforcing that here rather than at each call site means a
// future ordering or sorting option cannot accidentally bury the baseline row in
// one format while leaving it in place in another.
type Renderer interface {
	Render(w io.Writer, findings []scan.Finding) error
	// RenderScans writes a seed's scan history, so `yatt history` honours
	// --output exactly as `yatt scan` does.
	RenderScans(w io.Writer, scans []store.Scan) error
	// RenderTriage writes recorded verdicts, so `yatt triage` honours --output
	// too and a triage listing is as pipeable as a scan.
	RenderTriage(w io.Writer, entries []store.Triage) error
}

// New returns the renderer for the named format.
func New(format string) (Renderer, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "table":
		return TableRenderer{}, nil
	case "json":
		return JSONRenderer{}, nil
	case "ndjson":
		return NDJSONRenderer{}, nil
	default:
		return nil, fmt.Errorf("unknown output format %q (available: %s)", format, strings.Join(Formats, ", "))
	}
}

// TableRenderer writes an aligned, human-readable table.
type TableRenderer struct{}

// Render implements Renderer.
func (TableRenderer) Render(w io.Writer, findings []scan.Finding) error {
	findings = scan.SeedFirst(findings)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "CANDIDATE\tTECHNIQUE\tDIFF\tTRIAGE\tREGISTERED\tNS\tMX\tA\tWILDCARD\tADDRESSES"); err != nil {
		return err
	}
	for _, f := range findings {
		addresses := strings.Join(f.Addresses, ",")
		if f.Error != "" && addresses == "" {
			addresses = "error: " + f.Error
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			f.Candidate, f.Technique, dash(string(f.Diff)), dash(string(f.Triage)),
			yesNo(f.Registered), yesNo(f.HasNS), yesNo(f.HasMX), yesNo(f.HasA),
			yesNo(f.Wildcard), addresses,
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// RenderTriage implements Renderer.
func (TableRenderer) RenderTriage(w io.Writer, entries []store.Triage) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "CANDIDATE\tSTATUS\tUPDATED\tNOTE"); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			e.Candidate, e.Status, e.UpdatedAt.Local().Format(time.RFC3339), dash(e.Note),
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// RenderScans implements Renderer.
func (TableRenderer) RenderScans(w io.Writer, scans []store.Scan) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "SCAN\tSTARTED\tPROFILE\tCANDIDATES\tREGISTERED"); err != nil {
		return err
	}
	for _, s := range scans {
		if _, err := fmt.Fprintf(tw, "%d\t%s\t%s\t%d\t%d\n",
			s.ID, s.CreatedAt.Local().Format(time.RFC3339), dash(s.Profile),
			s.Candidates, s.Registered,
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// JSONRenderer writes the findings as one indented JSON array.
type JSONRenderer struct{}

// Render implements Renderer.
func (JSONRenderer) Render(w io.Writer, findings []scan.Finding) error {
	if findings == nil {
		findings = []scan.Finding{}
	}
	return encodeIndented(w, scan.SeedFirst(findings))
}

// RenderScans implements Renderer.
func (JSONRenderer) RenderScans(w io.Writer, scans []store.Scan) error {
	if scans == nil {
		scans = []store.Scan{}
	}
	return encodeIndented(w, scans)
}

// RenderTriage implements Renderer.
func (JSONRenderer) RenderTriage(w io.Writer, entries []store.Triage) error {
	if entries == nil {
		entries = []store.Triage{}
	}
	return encodeIndented(w, entries)
}

// NDJSONRenderer writes one compact JSON object per line, for piping.
type NDJSONRenderer struct{}

// Render implements Renderer.
func (NDJSONRenderer) Render(w io.Writer, findings []scan.Finding) error {
	enc := json.NewEncoder(w)
	for _, f := range scan.SeedFirst(findings) {
		if err := enc.Encode(f); err != nil {
			return err
		}
	}
	return nil
}

// RenderScans implements Renderer.
func (NDJSONRenderer) RenderScans(w io.Writer, scans []store.Scan) error {
	enc := json.NewEncoder(w)
	for _, s := range scans {
		if err := enc.Encode(s); err != nil {
			return err
		}
	}
	return nil
}

// RenderTriage implements Renderer.
func (NDJSONRenderer) RenderTriage(w io.Writer, entries []store.Triage) error {
	enc := json.NewEncoder(w)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// Summarize returns a one-line summary of a finding set, for the human-facing
// formats.
//
// The seed's own row is left out of every count: it is not a candidate, and
// folding it in would inflate "N candidates" by one and "N registered" by one on
// every scan of a domain that exists. A movement in the seed's own signals is
// called out separately instead, since that is a fact about the domain being
// protected rather than about the look-alikes.
func Summarize(findings []scan.Finding) string {
	var candidates, registered, errored, triaged, wildcards int
	var seedChanged bool
	counts := map[scan.DiffStatus]int{}
	for _, f := range findings {
		if f.IsOriginal() {
			seedChanged = f.Diff == scan.DiffChanged
			continue
		}
		candidates++
		if f.Registered {
			registered++
		}
		if f.Wildcard {
			wildcards++
		}
		if f.Error != "" {
			errored++
		}
		if f.Diff != "" {
			counts[f.Diff]++
		}
		if f.Triage.Triaged() {
			triaged++
		}
	}
	summary := fmt.Sprintf("%d candidates, %d registered", candidates, registered)
	if counts[scan.DiffNew] > 0 || counts[scan.DiffChanged] > 0 {
		summary += fmt.Sprintf(", %d new, %d changed", counts[scan.DiffNew], counts[scan.DiffChanged])
	}
	// Only worth saying once someone has actually judged something; on a first
	// scan a "0 triaged" would be noise.
	if triaged > 0 {
		summary += fmt.Sprintf(", %d triaged", triaged)
	}
	// Said out loud because a high wildcard count is the explanation for a
	// suspiciously good scan: the zone answers for everything, not the squatters.
	if wildcards > 0 {
		summary += fmt.Sprintf(", %d wildcard", wildcards)
	}
	if errored > 0 {
		summary += fmt.Sprintf(", %d errored", errored)
	}
	// Worth its own clause: the seed losing its MX or changing nameservers is a
	// bigger deal than any single look-alike moving, and a count would hide it.
	if seedChanged {
		summary += ", the seed's own signals changed"
	}
	return summary
}

func encodeIndented(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "-"
}

// dash renders an absent optional value as a placeholder, so a column is never
// silently blank.
func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
