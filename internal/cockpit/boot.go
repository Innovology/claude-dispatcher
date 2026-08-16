package cockpit

// boot.go is the opening screen's state: the console-boot sequence the cockpit
// runs through while its first snapshot is being built.
//
// The screen exists because that first load is not instant — it walks the scan
// roots, asks the supervisor which sessions are still running, reads every
// dispatch record's transcript and diff, and talks to the forge. Until now the
// cockpit showed an empty frame for those seconds, which reads as "nothing
// here" rather than "still counting".
//
// The discipline is the same as everywhere else in this package: every step on
// the screen is a real stage of loadSnapshot, and every figure beside it is
// what that stage actually found. Nothing is padded out to fill the list, and a
// stage whose source is missing says so (warn) rather than reporting a zero as
// if it had looked and found nothing. The one concession to theatre is timing —
// see bootMinShow — and it delays the cockpit, never the data.

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Step ids. They are the join between loadSnapshot, which reports against them,
// and the screen, which draws them in bootSequence's order.
const (
	bootSupervisor  = "supervisor"
	bootRecords     = "records"
	bootSessions    = "sessions"
	bootRepos       = "repos"
	bootForge       = "forge"
	bootDispatchers = "dispatchers"
	bootProducts    = "products"
	bootBacklog     = "backlog"
	bootDecisions   = "decisions"
	bootUsage       = "usage"
	bootVelocity    = "velocity"
	bootStale       = "stale"
)

// bootSequence is the POST list, in the order loadSnapshot works through it.
// Renaming a label is cosmetic; reordering it is a lie, because the screen
// claims to show what is happening as it happens.
var bootSequence = []struct{ id, label string }{
	{bootSupervisor, "SUPERVISOR"},
	{bootRecords, "DISPATCH RECORDS"},
	{bootSessions, "LIVE SESSIONS"},
	{bootRepos, "REPOSITORIES"},
	{bootForge, "FORGE"},
	{bootDispatchers, "DISPATCHERS"},
	{bootProducts, "PRODUCTS"},
	{bootBacklog, "BACKLOG"},
	{bootDecisions, "DECISIONS"},
	{bootUsage, "USAGE"},
	{bootVelocity, "VELOCITY"},
	{bootStale, "STALE WORK"},
}

const (
	bootPending = iota
	bootRunning
	bootDone
)

// Timing. bootTickEvery paces the spinner and the wordmark's colour sweep.
//
// bootMinShow is the one deliberate delay in the cockpit: on a small portfolio
// the whole load can finish inside 200ms, and a screen that appears and
// vanishes inside a blink reads as a glitch rather than as a boot. It holds the
// opening screen up to this long in total — not longer, and any key skips
// straight past it, so the floor is never a wall.
const (
	bootTickEvery   = 90 * time.Millisecond
	bootReadyLinger = 650 * time.Millisecond
	bootMinShow     = 1200 * time.Millisecond
)

// bootChanBuffer is comfortably more than the two updates per step the sequence
// can produce, so the loader's non-blocking sends never actually drop one in
// practice while still being unable to stall the load if nobody is listening.
const bootChanBuffer = 64

// bootUpdate is one step transition, sent from the loader goroutine to the UI.
// complete=false means the step just started and detail says what it is doing;
// complete=true means it finished and detail says what it found.
type bootUpdate struct {
	id       string
	detail   string
	warn     bool
	complete bool
}

// bootReport is loadSnapshot's channel back to the opening screen. A nil
// reporter is the normal case — every refresh after the first has no screen to
// report to — so both methods are nil-safe and the collectors never branch.
type bootReport func(bootUpdate)

// begin marks a step as running, with what it is about to do.
func (r bootReport) begin(id, doing string) {
	if r != nil {
		r(bootUpdate{id: id, detail: doing})
	}
}

// done marks a step finished, with what it found. warn says the step's source
// was unavailable, so its figure is an absence rather than a count.
func (r bootReport) done(id, found string, warn bool) {
	if r != nil {
		r(bootUpdate{id: id, detail: found, warn: warn, complete: true})
	}
}

// bootStep is one line of the sequence as the screen holds it.
type bootStep struct {
	id, label string
	state     int
	detail    string
	warn      bool
	started   time.Time
	elapsed   time.Duration
}

// bootState is the opening screen. It lives behind a pointer on the model so
// the loader's updates survive Bubble Tea's value-copied model.
type bootState struct {
	steps   []bootStep
	frame   int
	started time.Time
	ready   bool
}

func newBootState() *bootState {
	steps := make([]bootStep, len(bootSequence))
	for i, d := range bootSequence {
		steps[i] = bootStep{id: d.id, label: d.label}
	}
	return &bootState{steps: steps, started: time.Now()}
}

// apply folds one update into the sequence. An id nothing matches is dropped:
// the screen shows the steps it knows about and never grows a row mid-boot.
func (b *bootState) apply(u bootUpdate) {
	for i := range b.steps {
		s := &b.steps[i]
		if s.id != u.id {
			continue
		}
		s.detail, s.warn = u.detail, u.warn
		if u.complete {
			s.state = bootDone
			if !s.started.IsZero() {
				s.elapsed = time.Since(s.started)
			}
		} else {
			s.state = bootRunning
			s.started = time.Now()
		}
		return
	}
}

// finish is called when the snapshot lands. Any step still pending is marked
// done without a figure — the load is over, so a spinner left turning would be
// claiming work that is not happening. It returns the tick that hands the
// terminal to the cockpit.
func (b *bootState) finish() tea.Cmd {
	if b.ready {
		return nil // a second snapshot arrived before the linger elapsed
	}
	for i := range b.steps {
		if b.steps[i].state == bootDone {
			continue
		}
		// The step ran — the snapshot it contributed to is here — but its
		// report never reached the screen. Say that, rather than leave a tick
		// beside a blank where every other line carries a figure.
		b.steps[i].state = bootDone
		b.steps[i].detail = "—"
		b.steps[i].elapsed = 0
	}
	b.ready = true
	return tea.Tick(b.linger(), func(time.Time) tea.Msg { return bootDoneMsg{} })
}

// linger is how long READY stays up: long enough for the whole screen to have
// been visible for bootMinShow, and never less than a beat on READY itself.
func (b *bootState) linger() time.Duration {
	d := bootMinShow - time.Since(b.started)
	if d < bootReadyLinger {
		d = bootReadyLinger
	}
	return d
}

// progress counts finished steps against the whole sequence.
func (b *bootState) progress() (done, total int) {
	for _, s := range b.steps {
		if s.state == bootDone {
			done++
		}
	}
	return done, len(b.steps)
}

// active is the index of the step the loader is on — the running one, or the
// last one to finish. It is what the screen scrolls to keep in view.
func (b *bootState) active() int {
	last := 0
	for i, s := range b.steps {
		if s.state == bootRunning {
			return i
		}
		if s.state == bootDone {
			last = i
		}
	}
	return last
}

func bootTick() tea.Cmd {
	return tea.Tick(bootTickEvery, func(time.Time) tea.Msg { return bootTickMsg{} })
}
