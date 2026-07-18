package render_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/andoniaf/yatt/internal/render"
	"github.com/andoniaf/yatt/internal/scan"
	"github.com/andoniaf/yatt/internal/store"
	"github.com/andoniaf/yatt/internal/triage"
	"github.com/andoniaf/yatt/pkg/engine"
)

func sampleFindings() []scan.Finding {
	return []scan.Finding{
		{
			Candidate:   "xample.com",
			Registrable: "xample.com",
			Technique:   "omission",
			Registered:  true,
			HasNS:       true,
			HasA:        true,
			HasMX:       false,
			Addresses:   []string{"192.0.2.1"},
			NS:          []string{"ns1.example.net"},
			Rcode:       "NOERROR",
			Diff:        scan.DiffChanged,
			Triage:      triage.StatusSuspicious,
			TriageNote:  "parked on a squatter nameserver",
		},
		{
			Candidate:   "eample.com",
			Registrable: "eample.com",
			Technique:   "omission",
			Rcode:       "NXDOMAIN",
			Diff:        scan.DiffNew,
			Triage:      triage.StatusNew,
		},
	}
}

// findingsWithTrailingSeed is what a renderer must cope with: the seed's own row
// arriving somewhere other than first, because some later ordering moved it.
func findingsWithTrailingSeed() []scan.Finding {
	return append(sampleFindings(), scan.Finding{
		Candidate:   "example.com",
		Registrable: "example.com",
		Technique:   engine.TechniqueOriginal,
		Registered:  true,
		HasNS:       true,
		HasA:        true,
		HasMX:       true,
		Addresses:   []string{"192.0.2.10"},
		Rcode:       "NOERROR",
		Diff:        scan.DiffUnchanged,
	})
}

func sampleTriage() []store.Triage {
	updated := time.Date(2026, 7, 18, 9, 30, 0, 0, time.UTC)
	return []store.Triage{
		{
			Seed:      "example.com",
			Candidate: "xample.com",
			Status:    triage.StatusSuspicious,
			Note:      "parked on a squatter nameserver",
			UpdatedAt: updated,
		},
		{
			Seed:      "example.com",
			Candidate: "eample.com",
			Status:    triage.StatusOwned,
			UpdatedAt: updated.Add(-time.Hour),
		},
	}
}

func sampleScans() []store.Scan {
	created := time.Date(2026, 7, 18, 9, 30, 0, 0, time.UTC)
	return []store.Scan{
		{ID: 2, Seed: "example.com", CreatedAt: created.Add(time.Hour), Candidates: 6, Registered: 2},
		{ID: 1, Seed: "example.com", Profile: "quick", CreatedAt: created, Candidates: 6, Registered: 1},
	}
}

func TestNewUnknownFormat(t *testing.T) {
	if _, err := render.New("yaml"); err == nil {
		t.Fatal("New(\"yaml\") succeeded, want an error")
	}
}

func TestNewDefaultsToTable(t *testing.T) {
	renderer, err := render.New("")
	if err != nil {
		t.Fatalf("New(\"\"): %v", err)
	}
	if _, ok := renderer.(render.TableRenderer); !ok {
		t.Errorf("New(\"\") = %T, want render.TableRenderer", renderer)
	}
}

func TestTableRender(t *testing.T) {
	var buf bytes.Buffer
	if err := (render.TableRenderer{}).Render(&buf, sampleFindings()); err != nil {
		t.Fatalf("Render: %v", err)
	}

	want := strings.Join([]string{
		"CANDIDATE   TECHNIQUE  DIFF     TRIAGE      REGISTERED  NS   MX  A    WILDCARD  ADDRESSES",
		"xample.com  omission   changed  suspicious  yes         yes  -   yes  -         192.0.2.1",
		"eample.com  omission   new      new         -           -    -   -    -         ",
		"",
	}, "\n")

	if got := buf.String(); got != want {
		t.Errorf("Render() =\n%q\nwant\n%q", got, want)
	}
}

func TestTableRenderMarksAbsentDiffStatus(t *testing.T) {
	var buf bytes.Buffer
	findings := []scan.Finding{{Candidate: "xample.com", Technique: "omission"}}
	if err := (render.TableRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render: %v", err)
	}
	// A stateless run leaves the diff column empty; it must still be a
	// placeholder, so the column never silently shifts the table.
	if !strings.Contains(buf.String(), "DIFF") || !strings.Contains(buf.String(), "-") {
		t.Errorf("Render() = %q, want a placeholder in the diff column", buf.String())
	}
}

func TestTableRenderScans(t *testing.T) {
	var buf bytes.Buffer
	if err := (render.TableRenderer{}).RenderScans(&buf, sampleScans()); err != nil {
		t.Fatalf("RenderScans: %v", err)
	}

	got := buf.String()
	for _, want := range []string{"SCAN", "STARTED", "PROFILE", "CANDIDATES", "REGISTERED", "quick"} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderScans() = %q, want it to contain %q", got, want)
		}
	}
	// The scan with no profile recorded must show a placeholder, not a gap.
	if lines := strings.Split(strings.TrimSpace(got), "\n"); len(lines) != 3 {
		t.Errorf("RenderScans() produced %d lines, want a header plus 2 rows:\n%s", len(lines), got)
	}
}

func TestJSONRenderScans(t *testing.T) {
	var buf bytes.Buffer
	if err := (render.JSONRenderer{}).RenderScans(&buf, sampleScans()); err != nil {
		t.Fatalf("RenderScans: %v", err)
	}

	var got []store.Scan
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if len(got) != 2 || got[0].ID != 2 {
		t.Errorf("decoded %+v, want the two sample scans, most recent first", got)
	}
}

func TestJSONRenderScansEmptyIsAnArrayNotNull(t *testing.T) {
	var buf bytes.Buffer
	if err := (render.JSONRenderer{}).RenderScans(&buf, nil); err != nil {
		t.Fatalf("RenderScans: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "[]" {
		t.Errorf("RenderScans() = %q, want %q", got, "[]")
	}
}

func TestNDJSONRenderScansOneObjectPerLine(t *testing.T) {
	var buf bytes.Buffer
	if err := (render.NDJSONRenderer{}).RenderScans(&buf, sampleScans()); err != nil {
		t.Fatalf("RenderScans: %v", err)
	}
	if lines := strings.Split(strings.TrimSpace(buf.String()), "\n"); len(lines) != 2 {
		t.Errorf("got %d lines, want 2:\n%s", len(lines), buf.String())
	}
}

func TestJSONRenderCarriesDiffStatus(t *testing.T) {
	var buf bytes.Buffer
	if err := (render.JSONRenderer{}).Render(&buf, sampleFindings()); err != nil {
		t.Fatalf("Render: %v", err)
	}

	var got []scan.Finding
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if got[0].Diff != scan.DiffChanged || got[1].Diff != scan.DiffNew {
		t.Errorf("diff statuses = %q, %q, want %q, %q",
			got[0].Diff, got[1].Diff, scan.DiffChanged, scan.DiffNew)
	}
	if !strings.Contains(buf.String(), `"diff": "changed"`) {
		t.Errorf("diff is not a first-class JSON field:\n%s", buf.String())
	}
}

func TestTableRenderMarksAbsentTriageStatus(t *testing.T) {
	var buf bytes.Buffer
	// A stateless run reads no verdicts, so the column has nothing to say. It
	// must still be present, or the table silently loses a column between a
	// stored and an unstored run.
	findings := []scan.Finding{{Candidate: "xample.com", Technique: "omission"}}
	if err := (render.TableRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(buf.String(), "TRIAGE") {
		t.Errorf("Render() = %q, want a TRIAGE column", buf.String())
	}
}

func TestJSONRenderCarriesTriageStatus(t *testing.T) {
	var buf bytes.Buffer
	if err := (render.JSONRenderer{}).Render(&buf, sampleFindings()); err != nil {
		t.Fatalf("Render: %v", err)
	}

	var got []scan.Finding
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if got[0].Triage != triage.StatusSuspicious || got[1].Triage != triage.StatusNew {
		t.Errorf("triage statuses = %q, %q, want %q, %q",
			got[0].Triage, got[1].Triage, triage.StatusSuspicious, triage.StatusNew)
	}
	if got[0].TriageNote != "parked on a squatter nameserver" {
		t.Errorf("triage note = %q, want the stored note", got[0].TriageNote)
	}
	if !strings.Contains(buf.String(), `"triage": "suspicious"`) {
		t.Errorf("triage is not a first-class JSON field:\n%s", buf.String())
	}
}

func TestTableRenderMarksWildcardCandidates(t *testing.T) {
	var buf bytes.Buffer
	findings := []scan.Finding{
		{Candidate: "xample.com", Technique: "omission", Registered: true, HasA: true,
			Addresses: []string{"192.0.2.1"}, Wildcard: true},
		{Candidate: "eample.com", Technique: "omission", Registered: true, HasA: true,
			Addresses: []string{"198.51.100.7"}},
	}
	if err := (render.TableRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("Render() produced %d lines, want a header plus 2 rows:\n%s", len(lines), buf.String())
	}
	// The column is located by name rather than by position or padding width:
	// tabwriter sizes each column to its widest cell, so asserting on literal
	// runs of spaces breaks whenever an unrelated column grows.
	wildcardAt := columnIndex(t, lines[0], "WILDCARD")

	// Both rows resolve, so only this column tells them apart: without it a
	// catch-all zone reads as a page of live look-alikes.
	if got := strings.Fields(lines[1])[wildcardAt]; got != "yes" {
		t.Errorf("wildcard row: WILDCARD = %q, want %q\n%s", got, "yes", lines[1])
	}
	if got := strings.Fields(lines[2])[wildcardAt]; got != "-" {
		t.Errorf("real row: WILDCARD = %q, want it blank\n%s", got, lines[2])
	}
}

// columnIndex returns the field position of a named column in a table header.
func columnIndex(t *testing.T, header, name string) int {
	t.Helper()
	for i, field := range strings.Fields(header) {
		if field == name {
			return i
		}
	}
	t.Fatalf("header has no %s column: %q", name, header)
	return -1
}

func TestJSONRenderCarriesWildcard(t *testing.T) {
	var buf bytes.Buffer
	findings := []scan.Finding{{Candidate: "xample.com", Technique: "omission", Wildcard: true}}
	if err := (render.JSONRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render: %v", err)
	}

	var got []scan.Finding
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if !got[0].Wildcard {
		t.Error("Wildcard = false after a JSON round-trip, want true")
	}
	if !strings.Contains(buf.String(), `"wildcard": true`) {
		t.Errorf("wildcard is not a first-class JSON field:\n%s", buf.String())
	}
}

func TestTableRenderTriage(t *testing.T) {
	var buf bytes.Buffer
	if err := (render.TableRenderer{}).RenderTriage(&buf, sampleTriage()); err != nil {
		t.Fatalf("RenderTriage: %v", err)
	}

	got := buf.String()
	for _, want := range []string{"CANDIDATE", "STATUS", "UPDATED", "NOTE", "suspicious", "owned"} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderTriage() = %q, want it to contain %q", got, want)
		}
	}
	if lines := strings.Split(strings.TrimSpace(got), "\n"); len(lines) != 3 {
		t.Errorf("RenderTriage() produced %d lines, want a header plus 2 rows:\n%s", len(lines), got)
	}
}

func TestJSONRenderTriage(t *testing.T) {
	var buf bytes.Buffer
	if err := (render.JSONRenderer{}).RenderTriage(&buf, sampleTriage()); err != nil {
		t.Fatalf("RenderTriage: %v", err)
	}

	var got []store.Triage
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if len(got) != 2 || got[0].Candidate != "xample.com" || got[0].Status != triage.StatusSuspicious {
		t.Errorf("decoded %+v, want the two sample verdicts", got)
	}
}

func TestJSONRenderTriageEmptyIsAnArrayNotNull(t *testing.T) {
	var buf bytes.Buffer
	if err := (render.JSONRenderer{}).RenderTriage(&buf, nil); err != nil {
		t.Fatalf("RenderTriage: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "[]" {
		t.Errorf("RenderTriage() = %q, want %q", got, "[]")
	}
}

func TestNDJSONRenderTriageOneObjectPerLine(t *testing.T) {
	var buf bytes.Buffer
	if err := (render.NDJSONRenderer{}).RenderTriage(&buf, sampleTriage()); err != nil {
		t.Fatalf("RenderTriage: %v", err)
	}
	if lines := strings.Split(strings.TrimSpace(buf.String()), "\n"); len(lines) != 2 {
		t.Errorf("got %d lines, want 2:\n%s", len(lines), buf.String())
	}
}

func TestTableRenderEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := (render.TableRenderer{}).Render(&buf, nil); err != nil {
		t.Fatalf("Render: %v", err)
	}
	// The header must still print, so an empty result set is distinguishable
	// from a crash.
	if got := buf.String(); !strings.HasPrefix(got, "CANDIDATE") {
		t.Errorf("Render() = %q, want a header row", got)
	}
}

func TestTableRenderShowsErrors(t *testing.T) {
	var buf bytes.Buffer
	findings := []scan.Finding{{Candidate: "xample.com", Technique: "omission", Error: "i/o timeout"}}
	if err := (render.TableRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(buf.String(), "i/o timeout") {
		t.Errorf("Render() = %q, want the error surfaced", buf.String())
	}
}

func TestJSONRenderIsValidAndComplete(t *testing.T) {
	var buf bytes.Buffer
	if err := (render.JSONRenderer{}).Render(&buf, sampleFindings()); err != nil {
		t.Fatalf("Render: %v", err)
	}

	var got []scan.Finding
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if len(got) != 2 {
		t.Fatalf("decoded %d findings, want 2", len(got))
	}
	if got[0].Candidate != "xample.com" || !got[0].Registered {
		t.Errorf("first finding = %+v, want xample.com registered", got[0])
	}
	if got[1].Registered {
		t.Errorf("second finding = %+v, want unregistered", got[1])
	}
}

func TestJSONRenderEmptyIsAnArrayNotNull(t *testing.T) {
	var buf bytes.Buffer
	if err := (render.JSONRenderer{}).Render(&buf, nil); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "[]" {
		t.Errorf("Render() = %q, want %q", got, "[]")
	}
}

func TestNDJSONRenderOneObjectPerLine(t *testing.T) {
	var buf bytes.Buffer
	if err := (render.NDJSONRenderer{}).Render(&buf, sampleFindings()); err != nil {
		t.Fatalf("Render: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), buf.String())
	}
	for i, line := range lines {
		var f scan.Finding
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}
}

// Every format leads with the seed, whatever order the findings arrive in: the
// guarantee is the renderers', so no upstream ordering can lose it in one format
// while keeping it in another.
func TestSeedIsRenderedFirstInEveryFormat(t *testing.T) {
	t.Run("table", func(t *testing.T) {
		var buf bytes.Buffer
		if err := (render.TableRenderer{}).Render(&buf, findingsWithTrailingSeed()); err != nil {
			t.Fatalf("Render: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) != 4 {
			t.Fatalf("got %d lines, want a header plus 3 rows:\n%s", len(lines), buf.String())
		}
		if !strings.HasPrefix(lines[1], "example.com") {
			t.Errorf("first row = %q, want the seed", lines[1])
		}
		if !strings.Contains(lines[1], engine.TechniqueOriginal) {
			t.Errorf("first row = %q, want it marked %q", lines[1], engine.TechniqueOriginal)
		}
	})

	t.Run("json", func(t *testing.T) {
		var buf bytes.Buffer
		if err := (render.JSONRenderer{}).Render(&buf, findingsWithTrailingSeed()); err != nil {
			t.Fatalf("Render: %v", err)
		}
		var got []scan.Finding
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
		}
		if len(got) != 3 {
			t.Fatalf("decoded %d findings, want 3", len(got))
		}
		if !got[0].IsOriginal() || got[0].Candidate != "example.com" {
			t.Errorf("first finding = %+v, want the seed", got[0])
		}
		// The candidates keep their own order below it.
		if got[1].Candidate != "xample.com" || got[2].Candidate != "eample.com" {
			t.Errorf("candidate order = %q, %q, want xample.com then eample.com",
				got[1].Candidate, got[2].Candidate)
		}
	})

	t.Run("ndjson", func(t *testing.T) {
		var buf bytes.Buffer
		if err := (render.NDJSONRenderer{}).Render(&buf, findingsWithTrailingSeed()); err != nil {
			t.Fatalf("Render: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) != 3 {
			t.Fatalf("got %d lines, want 3:\n%s", len(lines), buf.String())
		}
		var first scan.Finding
		if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
			t.Fatalf("first line is not valid JSON: %v", err)
		}
		if !first.IsOriginal() || first.Candidate != "example.com" {
			t.Errorf("first line = %+v, want the seed", first)
		}
	})
}

// A blank triage column on the seed is deliberate: "new" would file the domain
// being protected into the untriaged backlog.
func TestTableRendersTheSeedWithNoTriageStatus(t *testing.T) {
	var buf bytes.Buffer
	findings := []scan.Finding{{
		Candidate: "example.com",
		Technique: engine.TechniqueOriginal,
		Diff:      scan.DiffUnchanged,
	}}
	if err := (render.TableRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(buf.String(), "new") {
		t.Errorf("Render() = %q, want the seed's triage column blank rather than \"new\"", buf.String())
	}
}

func TestSummarize(t *testing.T) {
	tests := []struct {
		name     string
		findings []scan.Finding
		want     string
	}{
		{
			name:     "counts registered candidates and diff statuses",
			findings: sampleFindings(),
			want:     "2 candidates, 1 registered, 1 new, 1 changed, 1 triaged",
		},
		{
			name: "an untriaged candidate is not counted as triaged",
			findings: []scan.Finding{
				{Candidate: "a.com", Triage: triage.StatusNew},
				{Candidate: "b.com", Triage: triage.StatusOwned},
			},
			want: "2 candidates, 0 registered, 1 triaged",
		},
		{
			name:     "a stateless run reports no diff counts",
			findings: []scan.Finding{{Candidate: "a.com"}},
			want:     "1 candidates, 0 registered",
		},
		{
			name:     "empty",
			findings: nil,
			want:     "0 candidates, 0 registered",
		},
		{
			name:     "errors are called out",
			findings: []scan.Finding{{Candidate: "a.com", Error: "i/o timeout"}},
			want:     "1 candidates, 0 registered, 1 errored",
		},
		{
			// The seed is not a candidate and counting it would report one extra
			// candidate, and one extra registered domain, on every scan.
			name:     "the seed is not counted as a candidate",
			findings: findingsWithTrailingSeed(),
			want:     "2 candidates, 1 registered, 1 new, 1 changed, 1 triaged",
		},
		{
			// A scan of a catch-all zone otherwise reads as an unusually
			// successful one; the count is what says "the zone answered, not the
			// squatters".
			name: "wildcard candidates are counted",
			findings: []scan.Finding{
				{Candidate: "a.com", Registered: true, Wildcard: true},
				{Candidate: "b.com", Registered: true},
			},
			want: "2 candidates, 2 registered, 1 wildcard",
		},
		{
			name: "a movement in the seed's own signals is called out",
			findings: []scan.Finding{
				{
					Candidate: "example.com",
					Technique: engine.TechniqueOriginal,
					Diff:      scan.DiffChanged,
				},
				{Candidate: "a.com", Technique: "omission", Diff: scan.DiffUnchanged},
			},
			want: "1 candidates, 0 registered, the seed's own signals changed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := render.Summarize(tt.findings); got != tt.want {
				t.Errorf("Summarize() = %q, want %q", got, tt.want)
			}
		})
	}
}
