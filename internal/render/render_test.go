package render_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/andoniaf/yatt/internal/render"
	"github.com/andoniaf/yatt/internal/scan"
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
		},
		{
			Candidate:   "eample.com",
			Registrable: "eample.com",
			Technique:   "omission",
			Rcode:       "NXDOMAIN",
		},
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
		"CANDIDATE   TECHNIQUE  REGISTERED  NS   MX  A    ADDRESSES",
		"xample.com  omission   yes         yes  -   yes  192.0.2.1",
		"eample.com  omission   -           -    -   -    ",
		"",
	}, "\n")

	if got := buf.String(); got != want {
		t.Errorf("Render() =\n%q\nwant\n%q", got, want)
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
			name:     "counts registered candidates",
			findings: sampleFindings(),
			want:     "2 candidates, 1 registered",
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
