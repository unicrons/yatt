package triage_test

import (
	"strings"
	"testing"

	"github.com/unicrons/yatt/internal/triage"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  triage.Status
	}{
		{name: "exact", input: "suspicious", want: triage.StatusSuspicious},
		{name: "case is folded", input: "MALICIOUS", want: triage.StatusMalicious},
		{name: "surrounding space is trimmed", input: "  benign  ", want: triage.StatusBenign},
		{name: "underscore form", input: "false_positive", want: triage.StatusFalsePositive},
		// A hyphen is what a user actually types on a command line, so accepting
		// it is the difference between the flag working and the flag arguing.
		{name: "hyphen form", input: "false-positive", want: triage.StatusFalsePositive},
		{name: "hyphen form, mixed case", input: "False-Positive", want: triage.StatusFalsePositive},
		{name: "owned", input: "owned", want: triage.StatusOwned},
		{name: "new", input: "new", want: triage.StatusNew},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := triage.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseRejectsUnknownStatusAndListsTheValidOnes(t *testing.T) {
	for _, input := range []string{"", "   ", "bogus", "malicous", "suspicious!"} {
		_, err := triage.Parse(input)
		if err == nil {
			t.Fatalf("Parse(%q) succeeded, want an error", input)
		}
		// A rejected verdict with no list of alternatives just sends the user to
		// --help, so the error has to carry the vocabulary.
		for _, want := range []string{"benign", "suspicious", "malicious", "false_positive", "owned"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("Parse(%q) error = %q, want it to list %q", input, err, want)
			}
		}
	}
}

func TestParseAll(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []triage.Status
	}{
		{
			name:  "repeated flag",
			input: []string{"benign", "owned"},
			want:  []triage.Status{triage.StatusBenign, triage.StatusOwned},
		},
		{
			name:  "comma separated",
			input: []string{"benign,owned"},
			want:  []triage.Status{triage.StatusBenign, triage.StatusOwned},
		},
		{
			name:  "empty entries are skipped",
			input: []string{"", "benign", " , "},
			want:  []triage.Status{triage.StatusBenign},
		},
		{name: "nothing", input: nil, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := triage.ParseAll(tt.input)
			if err != nil {
				t.Fatalf("ParseAll(%v): %v", tt.input, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ParseAll(%v) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ParseAll(%v)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// One bad entry must sink the whole list: partially honouring a filter widens
// it, which for an exclusion means showing what the analyst asked to hide.
func TestParseAllRejectsTheWholeListOnOneBadEntry(t *testing.T) {
	if _, err := triage.ParseAll([]string{"benign", "bogus"}); err == nil {
		t.Fatal("ParseAll succeeded with an unknown status, want an error")
	}
}

func TestValidAndTriaged(t *testing.T) {
	tests := []struct {
		status      triage.Status
		valid       bool
		triaged     bool
		description string
	}{
		{status: triage.StatusNew, valid: true, triaged: false, description: "new is valid but not a judgement"},
		{status: "", valid: false, triaged: false, description: "unset"},
		{status: triage.StatusOwned, valid: true, triaged: true, description: "owned"},
		{status: triage.StatusFalsePositive, valid: true, triaged: true, description: "false positive"},
		{status: "bogus", valid: false, triaged: true, description: "unknown values are not judgements the enum knows"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := tt.status.Valid(); got != tt.valid {
				t.Errorf("%q.Valid() = %v, want %v", tt.status, got, tt.valid)
			}
			if got := tt.status.Triaged(); got != tt.triaged {
				t.Errorf("%q.Triaged() = %v, want %v", tt.status, got, tt.triaged)
			}
		})
	}
}

func TestCanTransition(t *testing.T) {
	tests := []struct {
		name    string
		from    triage.Status
		to      triage.Status
		wantErr bool
	}{
		{name: "untriaged to a verdict", from: "", to: triage.StatusSuspicious},
		{name: "new to a verdict", from: triage.StatusNew, to: triage.StatusMalicious},
		// Verdicts get revised constantly, and a tool that argues about it is a
		// tool that gets worked around.
		{name: "suspicious downgraded to benign", from: triage.StatusSuspicious, to: triage.StatusBenign},
		{name: "benign escalated to malicious", from: triage.StatusBenign, to: triage.StatusMalicious},
		{name: "owned to false positive", from: triage.StatusOwned, to: triage.StatusFalsePositive},
		// Re-setting the same verdict is how a note is amended.
		{name: "same status is idempotent", from: triage.StatusIgnored, to: triage.StatusIgnored},

		{name: "nothing may go back to new", from: triage.StatusBenign, to: triage.StatusNew, wantErr: true},
		{name: "not even new to new", from: triage.StatusNew, to: triage.StatusNew, wantErr: true},
		{name: "unknown target", from: triage.StatusBenign, to: "bogus", wantErr: true},
		{name: "unknown current", from: "bogus", to: triage.StatusBenign, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := triage.CanTransition(tt.from, tt.to)
			if tt.wantErr && err == nil {
				t.Fatalf("CanTransition(%q, %q) succeeded, want an error", tt.from, tt.to)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("CanTransition(%q, %q): %v", tt.from, tt.to, err)
			}
		})
	}
}

func TestListNamesEveryStatus(t *testing.T) {
	list := triage.List()
	for _, status := range triage.Statuses {
		if !strings.Contains(list, string(status)) {
			t.Errorf("List() = %q, want it to contain %q", list, status)
		}
	}
}

// A message rejecting a verdict must not offer the one value it is rejecting.
func TestListSettableOmitsNew(t *testing.T) {
	settable := triage.ListSettable()
	if strings.Contains(settable, string(triage.StatusNew)) {
		t.Errorf("ListSettable() = %q, want it to omit %q", settable, triage.StatusNew)
	}
	for _, status := range triage.Statuses {
		if status == triage.StatusNew {
			continue
		}
		if !strings.Contains(settable, string(status)) {
			t.Errorf("ListSettable() = %q, want it to contain %q", settable, status)
		}
	}
}

func TestCanTransitionToNewDoesNotOfferNew(t *testing.T) {
	err := triage.CanTransition(triage.StatusBenign, triage.StatusNew)
	if err == nil {
		t.Fatal("CanTransition to new succeeded, want an error")
	}
	// "(valid: ... )" must not list new; the bare word still appears in the
	// explanation, so check the vocabulary list specifically.
	_, list, ok := strings.Cut(err.Error(), "(valid: ")
	if !ok {
		t.Fatalf("error = %q, want it to list the valid statuses", err)
	}
	if strings.Contains(list, "new") {
		t.Errorf("valid list = %q, want it to omit new", list)
	}
}
