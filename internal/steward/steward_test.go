package steward

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/supervisor"
)

// fakeSup is the supervisor, per session name.
type fakeSup struct {
	running map[string]bool
	idle    map[string]bool // claude has exited in the session
	unknown bool            // the pane cannot be seen into
	started []string        // "name|dir|cmd"
	killed  []string
	trusted bool
}

func withFakes(t *testing.T, f *fakeSup, exe string) {
	t.Helper()
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	if f.running == nil {
		f.running = map[string]bool{}
	}
	if f.idle == nil {
		f.idle = map[string]bool{}
	}
	pa, ph, pn, pk, pt, pe, pi, pc, pd := available, hasSession, newSession, killSession, trustDir, executable, sessionIdle, loadConfig, discover
	t.Cleanup(func() {
		available, hasSession, newSession, killSession, trustDir, executable, sessionIdle, loadConfig, discover = pa, ph, pn, pk, pt, pe, pi, pc, pd
	})
	// The fakes are the supervisor, so it is there whether or not this machine
	// has tmux: the nix build runs the suite in a sandbox without it.
	available = func() bool { return true }
	sessionIdle = func(s supervisor.Session) (bool, bool) { return f.idle[s.Name], !f.unknown }
	hasSession = func(s supervisor.Session) bool { return f.running[s.Name] }
	newSession = func(s supervisor.Session, dir, cmd, env string) error {
		f.started = append(f.started, s.Name+"|"+dir+"|"+cmd)
		f.running[s.Name], f.idle[s.Name] = true, false
		return nil
	}
	killSession = func(s supervisor.Session) error {
		f.killed = append(f.killed, s.Name)
		f.running[s.Name] = false
		return nil
	}
	trustDir = func(string) bool { return f.trusted }
	executable = func() (string, error) { return exe, nil }
	loadConfig = func() (*config.Config, error) {
		return &config.Config{Products: map[string][]string{"player": {"player-app", "design-system"}, "tools": {"cockpit"}}}, nil
	}
	discover = func(*config.Config) []repos.Repo {
		return []repos.Repo{
			{Name: "player-app", Path: "/src/player-app/main", Product: "player"},
			{Name: "design-system", Path: "/src/design-system", Product: "player"},
			{Name: "cockpit", Path: "/src/cockpit", Product: "tools"},
		}
	}
}

func exePath(t *testing.T) string { return filepath.Join(t.TempDir(), "claude-dispatcher") }

func TestStartWritesTheBriefAndStartsTheSession(t *testing.T) {
	f := &fakeSup{trusted: true}
	exe := exePath(t)
	withFakes(t, f, exe)
	s, err := Start("player")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.started) != 1 || !strings.HasPrefix(f.started[0], "disp_steward_player|"+s.Dir()+"|") {
		t.Fatalf("started %q", f.started)
	}
	cmd := f.started[0]
	// tmux gives a session its server's environment, so a chosen store has to
	// ride the command or the steward reads the default one.
	if !strings.Contains(cmd, "CLAUDE_DISPATCHER_STATE="+"'"+os.Getenv("CLAUDE_DISPATCHER_STATE")+"'") {
		t.Errorf("the store is not carried into the session: %s", cmd)
	}
	if strings.Contains(cmd, "CLAUDE_DISPATCHER_ID") {
		t.Error("a steward is not a dispatcher; its hooks must not be attributed to one")
	}
	// The conversation is named at the start, so it can be reopened by id.
	if !strings.Contains(cmd, "--session-id '"+s.SessionID+"'") || len(s.SessionID) != 36 {
		t.Errorf("no session id to reopen by: %s", cmd)
	}
	if !strings.Contains(cmd, "--name 'steward-player'") {
		t.Errorf("dispatchers cannot message it by name: %s", cmd)
	}
	brief, err := os.ReadFile(filepath.Join(s.Dir(), "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		exe + " status --product player --next",
		"# You are the steward of player",
		"player-app — /src/player-app/main",
		"design-system — /src/design-system",
	} {
		if !strings.Contains(string(brief), want) {
			t.Errorf("brief lacks %q", want)
		}
	}
	if strings.Contains(string(brief), "/src/cockpit") {
		t.Error("the brief names another product's repo")
	}
	raw, err := os.ReadFile(filepath.Join(s.Dir(), ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Permissions struct{ Allow, Ask, Deny []string } `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	allowed := strings.Join(st.Permissions.Allow, "\n")
	for _, verb := range []string{"status", "reply", "note", "park"} {
		if !strings.Contains(allowed, "Bash("+exe+" "+verb+":*)") {
			t.Errorf("%s is not allowed without asking — the steward would stop on a prompt nobody watches", verb)
		}
	}
	if strings.Contains(allowed, "gh pr merge") || strings.Contains(allowed, "git push") {
		t.Error("the steward must not be allowed to merge or push outright")
	}
	if strings.Join(st.Permissions.Deny, ",") != "Edit,Write,NotebookEdit" {
		t.Errorf("deny = %v", st.Permissions.Deny)
	}
	// A ruling is the human's: recording one always asks, so the steward cannot
	// grant itself the merges it allows. The merge verb checks the ruling itself.
	if strings.Join(st.Permissions.Ask, ",") != "Bash("+exe+" ruling add:*)" {
		t.Errorf("ask = %v", st.Permissions.Ask)
	}
	if !strings.Contains(allowed, "Bash("+exe+" merge:*)") || strings.Contains(allowed, "ruling add") {
		t.Errorf("allow = %v", st.Permissions.Allow)
	}
	if got := StateOf(s); got != Live {
		t.Errorf("state = %s", got)
	}
}

// One steward per product is live at a time; another product's is its own.
func TestOneLiveStewardPerProduct(t *testing.T) {
	f := &fakeSup{trusted: true}
	withFakes(t, f, exePath(t))
	a, err := Start("player")
	if err != nil {
		t.Fatal(err)
	}
	again, err := Start("player")
	if !errors.Is(err, ErrRunning) || again.ID != a.ID || len(f.started) != 1 {
		t.Fatalf("a second start must leave the live steward alone: %v", err)
	}
	if _, err := Start("tools"); err != nil || len(f.started) != 2 {
		t.Fatalf("another product's steward is its own: %v", err)
	}
	if SessionName("player") == SessionName("tools") {
		t.Fatal("two products share a session")
	}
}

// A steward whose session went away without anyone ending it is interrupted,
// and stays so: nothing brings it back on its own. Starting fresh ends it; it
// stays on record and reopenable.
func TestAnInterruptedStewardWaitsForTheHuman(t *testing.T) {
	f := &fakeSup{trusted: true}
	withFakes(t, f, exePath(t))
	old, _ := Start("player")
	f.running["disp_steward_player"] = false // a reboot took tmux with it
	if got := StateOf(old); got != Interrupted {
		t.Fatalf("state = %s", got)
	}
	fresh, err := Start("player")
	if err != nil || fresh.ID == old.ID {
		t.Fatalf("a new start must be a new conversation: %v", err)
	}
	if cur := Current("player"); cur == nil || cur.ID != fresh.ID {
		t.Fatalf("current = %+v", cur)
	}
	if reread, _ := Find(old.ID); reread.EndedAt == nil {
		t.Fatal("the steward a new one replaced must be ended, not left current beside it")
	}
	if len(Of("player")) != 2 {
		t.Fatal("an older steward must stay on record")
	}
}

// claude exiting in the pane is an interrupted steward too: the shell it drops
// to cannot steward anything. A pane nobody can see into is taken at its word.
func TestAnExitedClaudeIsInterrupted(t *testing.T) {
	f := &fakeSup{trusted: true}
	withFakes(t, f, exePath(t))
	s, _ := Start("player")
	f.idle["disp_steward_player"] = true
	if got := StateOf(s); got != Interrupted {
		t.Fatalf("state = %s", got)
	}
	f.unknown = true
	if got := StateOf(s); got != Live {
		t.Fatalf("an unknown answer must not call a steward gone: %s", got)
	}
}

// Reopening resumes the same conversation in its own folder, its brief
// rewritten first; it refuses while another of the product's stewards is
// live, and replaces the shell a dead claude left behind.
func TestReopenResumesTheConversation(t *testing.T) {
	f := &fakeSup{trusted: true}
	withFakes(t, f, exePath(t))
	first, _ := Start("player")
	if _, err := End(first.ID); err != nil || len(f.killed) != 1 {
		t.Fatalf("end: %v %v", err, f.killed)
	}
	second, _ := Start("player")

	if _, err := Reopen(first.ID); err == nil || !strings.Contains(err.Error(), "end it first") {
		t.Fatalf("reopening beside a live steward must be refused: %v", err)
	}
	f.idle["disp_steward_player"] = true // second's claude exited
	s, err := Reopen(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := f.started[len(f.started)-1]
	if !strings.Contains(last, "--resume '"+first.SessionID+"'") || !strings.Contains(last, "|"+first.Dir()+"|") {
		t.Fatalf("not the same conversation in its own folder: %s", last)
	}
	if strings.Contains(last, "--session-id") {
		t.Fatal("a reopen must not start a new conversation")
	}
	if s.EndedAt != nil || StateOf(s) != Live {
		t.Fatalf("reopened steward state = %s", StateOf(s))
	}
	if reread, _ := Find(second.ID); reread.EndedAt == nil {
		t.Fatal("the interrupted steward a reopen replaced must be ended")
	}
	if _, err := Reopen(first.ID); !errors.Is(err, ErrRunning) {
		t.Fatalf("reopening the live one is a jump-in, not a restart: %v", err)
	}
}

func TestEnd(t *testing.T) {
	f := &fakeSup{trusted: true}
	withFakes(t, f, exePath(t))
	s, _ := Start("player")
	if _, err := EndProduct("player"); err != nil || len(f.killed) != 1 {
		t.Fatalf("end: %v %v", err, f.killed)
	}
	if reread, _ := Find(s.ID); StateOf(reread) != Ended {
		t.Fatalf("state = %s", StateOf(reread))
	}
	if _, err := End(s.ID); err == nil {
		t.Error("ending an ended steward must say so")
	}
	if _, err := EndProduct("player"); err == nil {
		t.Error("a product with no current steward has nothing to end")
	}
}

func TestStartUntrustedStillStarts(t *testing.T) {
	f := &fakeSup{trusted: false}
	withFakes(t, f, exePath(t))
	s, err := Start("player")
	if !errors.Is(err, ErrUntrusted) || s == nil || len(f.started) != 1 {
		t.Fatalf("got %v, started %v", err, f.started)
	}
	if Current("player") == nil {
		t.Fatal("an untrusted start is still a steward on record")
	}
}

func TestStartRefusesAGoRunBinary(t *testing.T) {
	f := &fakeSup{trusted: true}
	withFakes(t, f, "/tmp/go-build123/b001/exe/claude-dispatcher")
	if _, err := Start("player"); err == nil || len(f.started) != 0 {
		t.Fatalf("started a steward pointed at a temporary build: %v", err)
	}
}

// No dispatcher can ever be given a steward's session name.
func TestSessionCannotCollideWithADispatcher(t *testing.T) {
	for _, p := range []string{"player", "", "Some Product"} {
		if strings.HasPrefix(SessionName(p), "disp-") {
			t.Fatalf("%q is in the dispatchers' namespace", SessionName(p))
		}
	}
	if SessionName("") != SessionName(Unassigned) {
		t.Fatal("no product is the unassigned bucket")
	}
}

// The command line lists, starts, reopens and ends.
func TestRun(t *testing.T) {
	f := &fakeSup{trusted: true}
	withFakes(t, f, exePath(t))
	var out, errOut bytes.Buffer
	if code := Run(nil, &out, &errOut); code != 0 || !strings.Contains(out.String(), "no stewards yet") {
		t.Fatalf("list on nothing: %d %q", code, out.String())
	}
	out.Reset()
	if code := Run([]string{"new", "player"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "started") {
		t.Fatalf("new: %d %q %q", code, out.String(), errOut.String())
	}
	id := Current("player").ID
	out.Reset()
	if code := Run([]string{"end", "player"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "steward open "+id) {
		t.Fatalf("end: %d %q %q", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := Run([]string{"open", id}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "reopened") {
		t.Fatalf("open: %d %q %q", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := Run(nil, &out, &errOut); code != 0 || !strings.Contains(out.String(), id) || !strings.Contains(out.String(), "live") {
		t.Fatalf("list: %q", out.String())
	}
	if code := Run([]string{"bogus", "x"}, &out, &errOut); code == 0 {
		t.Fatal("an unknown argument must fail")
	}
}

// A new steward whose session would not start leaves nothing behind: no
// record to list, no folder to reopen.
func TestAFailedStartLeavesNothing(t *testing.T) {
	f := &fakeSup{trusted: true}
	withFakes(t, f, exePath(t))
	newSession = func(supervisor.Session, string, string, string) error {
		return errors.New("error connecting to /tmp/…/default (File name too long)")
	}
	if _, err := Start("player"); err == nil {
		t.Fatal("a session that would not start reported success")
	}
	if dirs, _ := filepath.Glob(filepath.Join(ProductDir("player"), "*")); len(dirs) != 0 || len(All()) != 0 {
		t.Fatalf("left behind %v", dirs)
	}
}

// The machine-wide steward an earlier build ran is ended and its switch
// removed — by the cockpit's poll and by `steward stop` — and a product's own
// steward is never mistaken for it.
func TestRetireLegacy(t *testing.T) {
	f := &fakeSup{trusted: true}
	withFakes(t, f, exePath(t))
	if _, err := Start("player"); err != nil {
		t.Fatal(err)
	}
	if retired, err := RetireLegacy(); retired || err != nil || len(f.killed) != 0 {
		t.Fatalf("nothing legacy to retire, got %v %v %v", retired, err, f.killed)
	}
	_ = os.MkdirAll(filepath.Dir(legacySwitch()), 0o755)
	_ = os.WriteFile(legacySwitch(), nil, 0o644)
	f.running[LegacySession] = true
	var out, errOut bytes.Buffer
	if code := Run([]string{"stop"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "retired") {
		t.Fatalf("steward stop: %d %q %q", code, out.String(), errOut.String())
	}
	if f.running[LegacySession] || !f.running["disp_steward_player"] {
		t.Fatalf("running = %v", f.running)
	}
	if _, err := os.Stat(legacySwitch()); !os.IsNotExist(err) {
		t.Fatal("the old switch is still there")
	}
	if code := Run([]string{"stop"}, &out, &errOut); code == 0 {
		t.Fatal("with nothing legacy left, a bare stop must ask for the product")
	}
}

// Names that slug alike stay apart: found in review, "Player App" and
// "player-app", or any two non-Latin names, shared one session and one rulings
// file — merge grants included. A name that is its own slug keeps it.
func TestProductNamesThatSlugAlikeStayApart(t *testing.T) {
	withFakes(t, &fakeSup{trusted: true}, exePath(t))
	if slug("playerpulse-player") != "playerpulse-player" || slug("") != "unassigned" {
		t.Fatal("a name that is its own slug must keep it")
	}
	for _, pair := range [][2]string{{"Player App", "player-app"}, {"Плеер", "Тренер"}, {"a b c d e f", "a b c d e g"}} {
		if slug(pair[0]) == slug(pair[1]) || SessionName(pair[0]) == SessionName(pair[1]) {
			t.Errorf("%q and %q share %q", pair[0], pair[1], slug(pair[0]))
		}
	}
	if _, err := AddRuling("Player App", Ruling{Text: "merge greens", Allow: []string{AllowMerge}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := MergeRuling("player-app", "app", time.Now()); ok {
		t.Fatal("one product's merge ruling granted another's")
	}
	// A ruling found by walking the folders is ended where it is, whatever
	// its folder is called.
	rs := Rulings("Player App")
	if _, _, err := EndRuling(rs[0].ID); err != nil || len(ActiveRulings("Player App", time.Now())) != 0 {
		t.Fatalf("end: %v", err)
	}
}

// A product the config does not name is refused, so a typo cannot start a
// steward, or record a ruling, for a product nothing groups under.
func TestUnknownProductRefused(t *testing.T) {
	f := &fakeSup{trusted: true}
	withFakes(t, f, exePath(t))
	if _, err := Start("plyer"); err == nil || !strings.Contains(err.Error(), "player") || len(f.started) != 0 {
		t.Fatalf("a typo started a steward: %v", err)
	}
	if _, err := Start(""); err != nil {
		t.Fatalf("the unassigned bucket is always a product: %v", err)
	}
	if CheckProduct(nil, "anything") != nil {
		t.Fatal("no config cannot say no")
	}
}
