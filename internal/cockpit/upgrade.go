package cockpit

// upgrade.go keeps the bottom-right corner honest about which build is running,
// and — when it is behind — runs the upgrade behind the cockpit rather than in
// front of it.
//
// `U` used to be a three-act sequence: a confirm bar, then the terminal handed
// to the package manager the way `enter` hands it to tmux, then a relaunch. The
// handover was the reason the confirm existed, and the handover has stopped
// being tenable: Homebrew 4.6 made ask mode the default, so the command the
// cockpit was handing the screen to now stops on "Proceed? [Y/n]" — a second
// question, on a screen the human had just been thrown out of, to install the
// thing they had already agreed to install.
//
// So the ask is answered where it belongs (version.Env / the winget flags — a
// command that cannot ask), the run happens off the UI goroutine, and the whole
// of it on screen is one bar above the footer. What the bar says is not ours:
// the meter claims motion and nothing else, and the words beside it are the
// package manager's own last line, verbatim. Then the new build is exec'd, the
// way it always was.

import (
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"claude-dispatcher/internal/version"
)

// upgradeMsg carries the newest published release tag to the UI goroutine.
// forced marks the answer a human asked for by pressing U, which is the only
// one that gets to speak: the poll's check is ambient and says nothing unless
// it has an upgrade to offer.
type upgradeMsg struct {
	latest string
	forced bool
}

// upgradeCheckCmd looks up the newest release off the UI goroutine. The lookup
// is cache-gated inside the version package, so firing it on every poll costs a
// file read rather than a network call. A dev build skips it entirely: there is
// no released version an unstamped build can be behind.
func upgradeCheckCmd() tea.Cmd {
	if !version.IsRelease() {
		return nil
	}
	return func() tea.Msg { return upgradeMsg{latest: version.Latest()} }
}

// upgradeRecheckCmd is the same lookup with the cache stepped over. It is what
// U runs when nothing newer is known: the cached answer is good for hours, and
// "you are on the latest" is the one thing we must not say on its word alone.
func upgradeRecheckCmd() tea.Cmd {
	return func() tea.Msg { return upgradeMsg{latest: version.Recheck(), forced: true} }
}

// upgradeRanMsg reports how the package manager got on. detail is its own last
// word on the subject — see upgradeRun.errLine.
type upgradeRanMsg struct {
	err    error
	detail string
}

// upgradeTickMsg paces the bar and is what finally execs the new build: it is
// the one message that arrives while an overlay has the keyboard, so it is the
// only place a held relaunch can be released from. See model.Update.
type upgradeTickMsg struct{}

const (
	// upgradeTickEvery paces the meter, at the boot screen's rate — the two are
	// the same kind of animation and must not beat against each other.
	upgradeTickEvery = 90 * time.Millisecond
	// upgradeLineMax caps a captured line. Homebrew prints tables and full URLs;
	// the bar is one line of a footer, and a caption that pushes the meter off
	// the screen has stopped being a caption.
	upgradeLineMax = 90
	// upgradeMeterRun is the lit run of the indeterminate meter, in cells.
	upgradeMeterRun = 8
)

// upgradeRun is the package manager running behind the cockpit.
//
// It lives behind a pointer on the model, like bootState, because Bubble Tea
// copies the model on every message and the writer goroutine has to reach the
// same object the renderer reads. Unlike bootState it is written from another
// goroutine, hence the mutex: `say` is called from the exec's output pumps and
// `status` from the UI.
type upgradeRun struct {
	cmd     []string
	to      string // the release being installed, for the bar's "→ v3.2.0"
	started time.Time
	frame   int // UI-goroutine only

	mu    sync.Mutex
	line  string // the manager's most recent line
	fault string // the last line that named an error
	done  bool   // installed; waiting for a moment when the exec takes nothing away
}

// say records the package manager's latest line. Only the latest is kept: the
// bar is a caption, not a log, and the renderer reads it at its own pace.
// A line that says nothing (a bare newline, a cleared progress line) is not
// allowed to blank a caption that did.
func (r *upgradeRun) say(s string) {
	if s = upgradeClean(s); s == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.line = s
	// Package managers say "Error: …" and then keep talking — cleanup notices,
	// a hint, a blank line — so the last line out is routinely not the one that
	// explains the failure. Keep the last one that did.
	if lower := strings.ToLower(s); strings.HasPrefix(lower, "error") ||
		strings.HasPrefix(lower, "fatal") || strings.Contains(lower, "error:") {
		r.fault = s
	}
}

func (r *upgradeRun) status() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.line
}

// errLine is what the failure notice quotes: the manager's own explanation if
// it gave one, otherwise the last thing it said before it gave up. Nothing is
// invented — an exit code with no output quotes nothing.
func (r *upgradeRun) errLine() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fault != "" {
		return r.fault
	}
	return r.line
}

// settle marks the install finished. The run stays on the model afterwards:
// the exec that follows it may have to wait (see model.inputPending), and the
// bar goes on saying so until it happens.
func (r *upgradeRun) settle() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done, r.line = true, ""
}

func (r *upgradeRun) settled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done
}

// upgradeClean makes one line of a package manager's output safe to draw into
// a fixed-width bar: no escape sequences (which dispWidth cannot measure and
// truncateAnsi would carry into the rest of the footer), no control characters,
// no more than upgradeLineMax columns.
func upgradeClean(s string) string {
	s = strings.TrimSpace(ansi.Strip(s))
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if dispWidth(s) > upgradeLineMax {
		s = truncate(s, upgradeLineMax)
	}
	return s
}

// upgradeWriter is the pump between one of the command's output streams and
// the run's caption.
//
// It breaks on \r as well as \n, deliberately: a download's percentage is
// carriage-returned over itself and never terminated, so splitting on newlines
// alone would hold the whole download in a buffer and show nothing, then dump
// it in one line at the end. Breaking on both is what makes the caption move
// while a cask is coming down the wire.
//
// stdout and stderr get one of these each — a writer per stream, so the buffer
// needs no lock of its own; the run behind them does the locking.
type upgradeWriter struct {
	run *upgradeRun
	buf []byte
}

func (w *upgradeWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		if b == '\n' || b == '\r' {
			w.run.say(string(w.buf))
			w.buf = w.buf[:0]
			continue
		}
		w.buf = append(w.buf, b)
		// A stream that never breaks must not grow without bound; flush what we
		// have and carry on rather than buffering a megabyte nobody will read.
		if len(w.buf) > 4096 {
			w.run.say(string(w.buf))
			w.buf = w.buf[:0]
		}
	}
	return len(p), nil
}

// upgradeRunCmd runs the package manager off the UI goroutine, reporting into
// run as it goes.
//
// Stdin is left nil, which exec connects to /dev/null. That is the backstop
// under the flags and the environment: whatever question we failed to answer in
// advance reads EOF and fails immediately, rather than waiting forever on an
// answer nobody can type. There is deliberately no timeout — the bar prints how
// long it has been going, so a wedged upgrade is visible rather than hidden, and
// killing a package manager part-way through an install is the one outcome
// worse than a stale binary.
func upgradeRunCmd(in version.Install, run *upgradeRun) tea.Cmd {
	return func() tea.Msg {
		c := exec.Command(in.Cmd[0], in.Cmd[1:]...)
		c.Env = in.Env()
		c.Stdout = &upgradeWriter{run: run}
		c.Stderr = &upgradeWriter{run: run}
		err := c.Run()
		return upgradeRanMsg{err: err, detail: run.errLine()}
	}
}

func upgradeTick() tea.Cmd {
	return tea.Tick(upgradeTickEvery, func(time.Time) tea.Msg { return upgradeTickMsg{} })
}

// versionForms is the bottom-right version, widest rendering first. The footer
// takes the first one that fits, so a cramped terminal keeps the version number
// and drops the upgrade offer rather than losing both.
//
// The offer names the key, not the command: the key is the shorter string and
// the one that does the work. An install we cannot upgrade in place still gets
// its clause — the version gap is worth knowing even when we have nothing to
// press — but it says why instead ("nix-managed · upgrade it where it is
// declared").
func (m model) versionForms() []string {
	if m.upgradeTo == "" {
		return []string{fg(cFaint, version.Display())}
	}
	behind := fg(cAmber, version.Display()+" → "+version.Label(m.upgradeTo))
	tail := m.install.Note
	if m.install.CanUpgrade() {
		tail = "U upgrades"
	}
	return []string{
		behind + fg(cDim, " · "+tail),
		behind,
	}
}

// startUpgrade is the U key. It runs the upgrade; it does not ask first.
//
// The confirm bar it used to open was there because the next thing that
// happened was the terminal being taken away — the human had to be told the
// screen was about to go. Nothing is taken away now, so what the confirm bought
// was a second agreement to the thing they had just pressed a key for.
func (m model) startUpgrade() (model, tea.Cmd) {
	if !version.IsRelease() {
		m.notice = "dev build — nothing to upgrade to"
		return m, nil
	}
	if m.upgrade != nil {
		// One at a time, and saying so: a second U during a download must not
		// start a second package manager on the same install. A settled run says
		// what it is actually waiting on, which is them.
		m.notice = "already upgrading — " + strings.Join(m.upgrade.cmd, " ")
		if m.upgrade.settled() {
			m.notice = "upgraded to " + version.Label(m.upgrade.to) +
				" — restarting as soon as you are done here"
		}
		return m, nil
	}
	if m.upgradeTo == "" {
		// Knowing of nothing newer is not the same as there being nothing newer.
		// The ambient answer is up to checkTTL old, and a human presses U because
		// they think a release is out — so go and look rather than reciting a
		// file. The answer comes back as a forced upgradeMsg, which either starts
		// the run below or says we are current, having actually checked.
		if m.upgradeChecking {
			return m, nil
		}
		m.upgradeChecking = true
		m.notice = "checking for a newer build…"
		return m, upgradeRecheckCmd()
	}
	if !m.install.CanUpgrade() {
		m.notice = m.install.Note
		return m, nil
	}
	run := &upgradeRun{cmd: m.install.Cmd, to: m.upgradeTo, started: time.Now()}
	run.say(strings.Join(m.install.Cmd, " "))
	m.upgrade = run
	m.notice = ""
	return m, tea.Batch(upgradeRunCmd(m.install, run), upgradeTick())
}

// inputPending reports whether the human has something on screen that the exec
// would take with it: text typed into a form, or a question waiting on their
// key. The upgrade now finishes while they are working, and a build installed
// mid-sentence must not be the reason a dispatch prompt they had spent five
// minutes on is gone.
//
// Reading and navigating are not on the list — help, review and resume lose
// nothing by being redrawn by a new build, and holding for them would mean an
// overlay left open at lunch stops the upgrade from ever landing. The triage
// form counts only once something has been typed into it (dxTouched), because
// on an empty fleet it is always on screen.
func (m model) inputPending() bool {
	return m.settings != nil ||
		m.dispatchForm != nil ||
		m.confirm != nil ||
		m.parkOpen ||
		m.replyOpen ||
		m.paletteOpen ||
		m.clNaming || m.clKeying ||
		(m.cqFormOn() && m.dxTouched())
}

// upgradeBar is the whole of the upgrade on screen: one line above the footer.
//
// The meter is indeterminate by construction — a lit run sliding through the
// track, wrapping. It claims that something is happening and nothing else,
// which is the only claim we can make: the package manager does not tell us how
// far through it is, and a bar filling to a schedule we invented would be a
// figure this cockpit does not print. What is beside it is measured — the
// manager's own line, and how long it has been going.
func (m model) upgradeBar() string {
	r := m.upgrade
	if r == nil {
		return ""
	}
	to := version.Display()
	if r.to != "" {
		to += " → " + version.Label(r.to)
	}

	word, colour, meter, tail := "upgrading", cBlue, upgradeMeter(r.frame, upgradeMeterW), r.status()
	if r.settled() {
		word, colour, meter = "upgraded", cGreen, strings.Repeat("█", upgradeMeterW)
		tail = "restarting on the new build"
		if m.inputPending() {
			tail = "restarting as soon as you are done here"
		}
	}

	left := fg(colour, word) + "  " + fg(cWhite, to) + "  " + fg(colour, meter)
	if tail != "" {
		left += "  " + fg(cDim, tail)
	}
	return spread(left, fg(cFaint, upgradeElapsed(time.Since(r.started))), m.width)
}

// upgradeMeterW is the meter's width in cells — a gauge, not a horizon, the
// same call the boot screen's meter makes.
const upgradeMeterW = 24

// upgradeMeter is the sliding run. It wraps, so the lit cells never bunch at an
// end and read as progress.
func upgradeMeter(frame, width int) string {
	if width < 1 {
		return ""
	}
	run := mini(upgradeMeterRun, width)
	head := (frame / 2) % width
	cells := make([]rune, width)
	for i := range cells {
		cells[i] = '░'
	}
	for i := 0; i < run; i++ {
		cells[(head+i)%width] = '█'
	}
	return string(cells)
}

// upgradeElapsed is how long the run has been going, at the resolution the tick
// can honour.
func upgradeElapsed(d time.Duration) string {
	s := int(d.Seconds())
	if s < 60 {
		return itoa(s) + "s"
	}
	return itoa(s/60) + "m " + itoa(s%60) + "s"
}

// upgradeFailed is the notice for a package manager that exited non-zero.
//
// Its output used to be on screen already — the cockpit had handed it the
// terminal — so this only had to name the command. Nothing is on screen now, so
// the reason has to come with it: detail is the manager's own last word (see
// upgradeRun.errLine), quoted rather than summarised, and omitted entirely when
// it said nothing.
func upgradeFailed(in version.Install, err error, detail string) string {
	msg := "upgrade failed (" + strings.Join(in.Cmd, " ") + "): " + err.Error()
	if detail != "" {
		msg += " · " + detail
	}
	return msg
}
