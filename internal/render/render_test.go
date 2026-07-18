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
		},
		{
			Candidate:   "eample.com",
			Registrable: "eample.com",
			Technique:   "omission",
			Rcode:       "NXDOMAIN",
			Diff:        scan.DiffNew,
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
		"CANDIDATE   TECHNIQUE  DIFF     REGISTERED  NS   MX  A    ADDRESSES",
		"xample.com  omission   changed  yes         yes  -   yes  192.0.2.1",
		"eample.com  omission   new      -           -    -   -    ",
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

func TestSummarize(t *testing.T) {
	tests := []struct {
		name     string
		findings []scan.Finding
		want     string
	}{
		{
			name:     "counts registered candidates and diff statuses",
			findings: sampleFindings(),
			want:     "2 candidates, 1 registered, 1 new, 1 changed",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := render.Summarize(tt.findings); got != tt.want {
				t.Errorf("Summarize() = %q, want %q", got, tt.want)
			}
		})
	}
}
