package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
)

// progressRedrawEvery throttles how often the line is rewritten. DNS answers
// arrive far faster than a human reads, and a terminal asked to repaint on
// every one of them flickers instead of informing.
const progressRedrawEvery = 100 * time.Millisecond

// progressBarWidth is the bar's fixed cell width. Fixed rather than
// terminal-sized because the stats tail already varies, and one moving part
// per line is enough.
const progressBarWidth = 30

// progressMinSamples is how many completions the ETA and speed wait for. An
// estimate extrapolated from one or two answers swings wildly enough to be
// worse than no estimate.
const progressMinSamples = 3

var progressStatsStyle = lipgloss.NewStyle().Faint(true)

// Progress renders a live, single-line progress report for a running scan,
// rewritten in place with a carriage return. It is not safe for concurrent
// use on its own: scan.Options.Progress serializes the calls.
type Progress struct {
	w   io.Writer
	bar progress.Model

	// now is the clock, a field so tests can drive the throttle and the
	// ETA/speed math deterministically.
	now func() time.Time

	start     time.Time
	lastDraw  time.Time
	drawn     bool
	lastWidth int
}

// NewProgress returns a reporter writing to w. The caller decides whether w
// is worth writing to at all — a reporter aimed at a pipe renders control
// characters nobody reads, so only attach one to a terminal.
func NewProgress(w io.Writer) *Progress {
	bar := progress.New(progress.WithDefaultGradient())
	bar.Width = progressBarWidth
	return &Progress{w: w, bar: bar, now: time.Now}
}

// Update redraws the line for the given totals. Redraws are throttled, except
// the final one — the last state drawn must be the true final state, not
// whichever intermediate one last beat the throttle.
func (p *Progress) Update(done, total, registered int) {
	now := p.now()
	if p.start.IsZero() {
		p.start = now
	}

	final := total > 0 && done >= total
	if p.drawn && !final && now.Sub(p.lastDraw) < progressRedrawEvery {
		return
	}
	p.draw(done, total, registered, now)
	p.drawn = true
	p.lastDraw = now
}

// Finish clears the line, so the report that follows starts on a clean row
// instead of over the bar's remains. A reporter that never drew writes
// nothing.
func (p *Progress) Finish() {
	if !p.drawn {
		return
	}
	_, _ = fmt.Fprint(p.w, "\r"+strings.Repeat(" ", p.lastWidth)+"\r")
}

func (p *Progress) draw(done, total, registered int, now time.Time) {
	var percent float64
	if total > 0 {
		percent = float64(done) / float64(total)
	}

	line := p.bar.ViewAs(percent) + " " + progressStatsStyle.Render(p.stats(done, total, registered, now))

	// A shrinking line is padded to the widest one drawn so far, or the tail
	// of the previous frame would survive the rewrite.
	if width := lipgloss.Width(line); width < p.lastWidth {
		line += strings.Repeat(" ", p.lastWidth-width)
	} else {
		p.lastWidth = width
	}
	_, _ = fmt.Fprint(p.w, "\r"+line)
}

// stats renders the textual tail: counts always, ETA and speed only once
// enough candidates have finished to make the extrapolation honest.
func (p *Progress) stats(done, total, registered int, now time.Time) string {
	s := fmt.Sprintf("%d/%d | registered: %d", done, total, registered)

	elapsed := now.Sub(p.start)
	if done < progressMinSamples || elapsed <= 0 {
		return s
	}

	// Overall average rather than a recent window: it is what the remaining
	// time actually divides by, and it does not jitter. Labeled d/s because
	// the unit is candidates resolved — each one costs several DNS queries,
	// so "qps" would understate the real query rate.
	speed := float64(done) / elapsed.Seconds()
	if remaining := total - done; remaining > 0 {
		eta := time.Duration(float64(remaining) / speed * float64(time.Second))
		s += " | eta: " + formatETA(eta)
	}
	return s + " | " + formatSpeed(speed)
}

func formatETA(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
}

func formatSpeed(speed float64) string {
	if speed >= 10 {
		return fmt.Sprintf("%.0f d/s", speed)
	}
	return fmt.Sprintf("%.1f d/s", speed)
}
