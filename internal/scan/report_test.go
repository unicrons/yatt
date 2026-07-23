package scan_test

import (
	"slices"
	"testing"

	"github.com/unicrons/yatt/internal/scan"
)

func reportFindings() []scan.Finding {
	return []scan.Finding{
		{Candidate: "example.com", Technique: "original", Registered: false},
		{Candidate: "a.com", Technique: "omission", Registered: false},
		{Candidate: "b.com", Technique: "omission", Registered: true},
		{Candidate: "c.com", Technique: "keyboard", Registered: false, Error: "timeout"},
		{Candidate: "d.com", Technique: "keyboard", Registered: true},
	}
}

func TestHideUnregistered(t *testing.T) {
	got := candidates(scan.HideUnregistered(reportFindings()))
	// The seed rides along despite being unregistered here, and so does c.com:
	// its lookup failed, so "not registered" was never actually established.
	want := []string{"example.com", "b.com", "c.com", "d.com"}

	if !slices.Equal(got, want) {
		t.Errorf("HideUnregistered = %v, want %v", got, want)
	}
}

func TestHideUnregisteredKeepsOrder(t *testing.T) {
	findings := []scan.Finding{
		{Candidate: "z.com", Registered: true},
		{Candidate: "a.com", Registered: false},
		{Candidate: "m.com", Registered: true},
	}

	got := candidates(scan.HideUnregistered(findings))
	if want := []string{"z.com", "m.com"}; !slices.Equal(got, want) {
		t.Errorf("HideUnregistered = %v, want %v (engine order, not sorted)", got, want)
	}
}

func TestRegisteredFirst(t *testing.T) {
	got := candidates(scan.RegisteredFirst(reportFindings()))
	// The seed leads regardless of its own registration; then the registered
	// candidates in engine order; then the rest, also in engine order.
	want := []string{"example.com", "b.com", "d.com", "a.com", "c.com"}

	if !slices.Equal(got, want) {
		t.Errorf("RegisteredFirst = %v, want %v", got, want)
	}
}

// TestRegisteredFirstIsStable guards the property the diff feature rests on:
// reordering the report must not reshuffle candidates within a group, or two
// scans over identical answers would render differently.
func TestRegisteredFirstIsStable(t *testing.T) {
	findings := []scan.Finding{
		{Candidate: "seed.com", Technique: "original"},
		{Candidate: "a.com", Registered: true},
		{Candidate: "b.com", Registered: true},
		{Candidate: "c.com", Registered: true},
	}

	first := candidates(scan.RegisteredFirst(findings))
	for range 5 {
		if got := candidates(scan.RegisteredFirst(findings)); !slices.Equal(got, first) {
			t.Fatalf("RegisteredFirst = %v on a repeat call, want %v every time", got, first)
		}
	}
	if want := []string{"seed.com", "a.com", "b.com", "c.com"}; !slices.Equal(first, want) {
		t.Errorf("RegisteredFirst = %v, want %v", first, want)
	}
}

// TestRegisteredFirstDoesNotMutateTheInput matters because the caller renders
// one slice and counts another; sorting in place would reorder both.
func TestRegisteredFirstDoesNotMutateTheInput(t *testing.T) {
	findings := reportFindings()
	before := candidates(findings)

	scan.RegisteredFirst(findings)

	if after := candidates(findings); !slices.Equal(before, after) {
		t.Errorf("input reordered to %v, want it left as %v", after, before)
	}
}
