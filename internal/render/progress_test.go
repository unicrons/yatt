package render

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// fakeClock is a hand-cranked clock, so the throttle and the ETA/speed math
// can be tested without sleeping.
type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestProgress(buf *bytes.Buffer) (*Progress, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)}
	p := NewProgress(buf)
	p.now = clock.Now
	return p, clock
}

// frames counts redraws: every frame starts with exactly one carriage return.
func frames(buf *bytes.Buffer) int {
	return strings.Count(buf.String(), "\r")
}

func TestProgressThrottlesRedraws(t *testing.T) {
	var buf bytes.Buffer
	p, clock := newTestProgress(&buf)

	p.Update(1, 100, 0)
	if got := frames(&buf); got != 1 {
		t.Fatalf("first update drew %d frames, want 1", got)
	}

	// Inside the throttle window nothing is redrawn, however many updates
	// arrive.
	clock.Advance(10 * time.Millisecond)
	p.Update(2, 100, 0)
	clock.Advance(10 * time.Millisecond)
	p.Update(3, 100, 0)
	if got := frames(&buf); got != 1 {
		t.Fatalf("throttled updates drew %d frames, want 1", got)
	}

	clock.Advance(progressRedrawEvery)
	p.Update(4, 100, 0)
	if got := frames(&buf); got != 2 {
		t.Fatalf("post-throttle update drew %d frames, want 2", got)
	}
}

func TestProgressFinalUpdateBeatsTheThrottle(t *testing.T) {
	var buf bytes.Buffer
	p, clock := newTestProgress(&buf)

	p.Update(99, 100, 0)
	clock.Advance(time.Millisecond)
	p.Update(100, 100, 7)

	if got := frames(&buf); got != 2 {
		t.Fatalf("final update drew %d frames, want 2 — the last state must always be drawn", got)
	}
	if !strings.Contains(buf.String(), "100/100") {
		t.Errorf("final frame missing %q:\n%q", "100/100", buf.String())
	}
}

func TestProgressStats(t *testing.T) {
	var buf bytes.Buffer
	p, clock := newTestProgress(&buf)

	// The first update starts the clock; too few samples for an estimate yet.
	p.Update(1, 100, 0)
	if got := buf.String(); strings.Contains(got, "eta:") || strings.Contains(got, "d/s") {
		t.Fatalf("one completion already produced an estimate:\n%q", got)
	}

	clock.Advance(10 * time.Second)
	buf.Reset()
	p.Update(20, 100, 4)

	got := buf.String()
	// 20 done in 10s: speed 2.0/s, 80 remaining, eta 40s.
	for _, want := range []string{"20/100", "registered: 4", "eta: 40s", "2.0 d/s"} {
		if !strings.Contains(got, want) {
			t.Errorf("frame missing %q:\n%q", want, got)
		}
	}
}

func TestProgressETAInMinutes(t *testing.T) {
	var buf bytes.Buffer
	p, clock := newTestProgress(&buf)

	p.Update(1, 1000, 0)
	clock.Advance(10 * time.Second)
	buf.Reset()
	p.Update(101, 1000, 0)

	got := buf.String()
	// 101 done in 10s: speed 10.1/s, 899 remaining, eta 89s.
	for _, want := range []string{"eta: 1m 29s", "10 d/s"} {
		if !strings.Contains(got, want) {
			t.Errorf("frame missing %q:\n%q", want, got)
		}
	}
}

func TestProgressFinishClearsTheLine(t *testing.T) {
	var buf bytes.Buffer
	p, clock := newTestProgress(&buf)

	p.Update(1, 10, 0)
	clock.Advance(progressRedrawEvery)
	p.Update(10, 10, 3)
	width := p.lastWidth

	buf.Reset()
	p.Finish()
	if got, want := buf.String(), "\r"+strings.Repeat(" ", width)+"\r"; got != want {
		t.Errorf("Finish wrote %q, want a cleared line %q", got, want)
	}
}

func TestProgressFinishWithoutDrawWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	p, _ := newTestProgress(&buf)

	p.Finish()
	if buf.Len() != 0 {
		t.Errorf("Finish on an unused reporter wrote %q, want nothing", buf.String())
	}
}
