// Package steward runs a product's lead: a Claude Code session of the
// dispatcher's own that keeps the product's dispatchers moving toward what they
// were sent to do, talks with the human about them, and leaves the human's calls
// to the human with the facts gathered.
//
// It is a session, not a loop in the cockpit, because what it does is
// judgement — "is this question one the brief already answers?" — and the
// cockpit's rule is that it never guesses. It is not a dispatcher: it has no
// dispatch record, no branch and no worktree, runs without
// CLAUDE_DISPATCHER_ID so its hooks are attributed to nothing, and never
// appears on the table it reads.
//
// There is one per product, not one for the machine (ADR 0029). A product is
// the unit the human thinks in and the unit work crosses repos in, and the one
// steward for everything was a session nobody opened: its first night it woke
// 141 times, wrote 139 notes and pushed nothing, while the human talked to a
// coordinator they had started by hand in one repo. A steward is also a
// conversation, so it has a history: each one is kept with the session id it
// was started under, and an older one is reopened rather than started over.
// At most one per product is current at a time; starting or reopening another
// ends it, since two leads coordinating one product would fight.
//
// Everything it knows and does goes through the fleet verbs
// (internal/fleetcmd), scoped to its product.
package steward

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// Unassigned is the product a repo in no [products] entry belongs to — the
// bucket the cockpit already keeps, so a machine with nothing assigned can
// still have a steward.
const Unassigned = "unassigned"

// Steward is one steward conversation for a product.
type Steward struct {
	ID      string `json:"id"`
	Product string `json:"product"`
	// SessionID is the claude conversation, chosen at the start
	// (--session-id) so that --resume can reopen it by the same id.
	SessionID string    `json:"session_id"`
	CreatedAt time.Time `json:"created_at"`
	// OpenedAt is the last start or reopen.
	OpenedAt time.Time `json:"opened_at"`
	// EndedAt is the steward being put down — by the human, or by another
	// steward of the product being started or reopened. Nil on the current
	// one, live or interrupted.
	EndedAt *time.Time `json:"ended_at,omitempty"`
}

// State is what a steward is right now.
type State string

const (
	// Live: the product's current steward, with claude running in its session.
	Live State = "live"
	// Interrupted: the current steward whose session went away without anyone
	// ending it — a reboot, a crash, claude exiting. Nothing brings it back on
	// its own: the human reopens it, or starts a new one.
	Interrupted State = "interrupted"
	// Ended: put down, and reopenable.
	Ended State = "ended"
)

// Key is the product a steward is named for: "" is Unassigned.
func Key(product string) string {
	if strings.TrimSpace(product) == "" {
		return Unassigned
	}
	return product
}

// slug is the product as a path segment and a session name.
func slug(product string) string {
	name := Key(product)
	s := dispatch.Slugify(name)
	if s == name {
		return s
	}
	// A slug that is not the name lost something on the way — case,
	// punctuation, words past the fifth, every non-Latin letter — and two
	// names can lose their way to the same one: "Player App" and
	// "player-app", or any two Cyrillic names, which slug to nothing. They
	// would share a session, and a rulings file with its merge grants, so
	// the name's own hash keeps them apart.
	sum := sha256.Sum256([]byte(name))
	h := hex.EncodeToString(sum[:3])
	if s == "" {
		return "product-" + h
	}
	return s + "-" + h
}

// CheckProduct refuses a product the config does not name. Every store here is
// keyed by product, so a typo would start a steward, or record a ruling or a
// question, for a product nothing groups under — a merge grant nobody can
// use, a question nobody is asked. A config that is not there cannot say, and
// is not taken to mean no.
func CheckProduct(cfg *config.Config, product string) error {
	product = Key(product)
	if product == Unassigned || cfg == nil {
		return nil
	}
	if _, ok := cfg.Products[product]; ok {
		return nil
	}
	names := []string{Unassigned}
	for p := range cfg.Products {
		names = append(names, p)
	}
	sort.Strings(names)
	return fmt.Errorf("no product %q — the config names %s", product, strings.Join(names, ", "))
}

// SessionName is the product's steward session. The underscore keeps it out of
// the dispatchers' namespace: theirs is "disp-" plus a slug of [a-z0-9-], so no
// feature can be given this name.
func SessionName(product string) string { return "disp_steward_" + slug(product) }

// PeerName is the name the product's steward goes by among Claude sessions on
// the machine (claude's --name): what a dispatcher messages it by.
func PeerName(product string) string { return "steward-" + slug(product) }

// session is a steward's supervisor address. It names no socket, so it lives on
// the default server: a product spans repos, and a repo's own server (ADR 0014)
// is for that repo's dispatchers.
func session(product string) supervisor.Session {
	return supervisor.Session{Name: SessionName(product)}
}

// ProductDir holds what belongs to a product rather than to one conversation:
// the rulings and the questions saved for the human, which a new steward of the
// product picks up where the last left them.
func ProductDir(product string) string {
	return filepath.Join(state.Dir(), "stewards", slug(product))
}

// Dir is the steward's own folder: its brief, its settings and its record. The
// conversation's transcript is keyed to it, so one folder per steward is what
// lets each be reopened on its own.
func (s *Steward) Dir() string { return filepath.Join(ProductDir(s.Product), s.ID) }

func (s *Steward) path() string { return filepath.Join(s.Dir(), "steward.json") }

// The supervisor and the world, as seams for tests.
var (
	available   = supervisor.Available
	sessionIdle = supervisor.SessionIdle
	hasSession  = supervisor.HasSession
	newSession  = supervisor.NewSession
	killSession = supervisor.KillSession
	trustDir    = dispatch.TrustOwnDir
	executable  = os.Executable
	loadConfig  = config.Load
	discover    = repos.Discover
	now         = time.Now
)

// All is every steward on record, the most recently opened first.
func All() []*Steward {
	paths, _ := filepath.Glob(filepath.Join(state.Dir(), "stewards", "*", "*", "steward.json"))
	var out []*Steward
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var s Steward
		if json.Unmarshal(raw, &s) != nil || s.ID == "" {
			continue
		}
		out = append(out, &s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].OpenedAt.After(out[j].OpenedAt) })
	return out
}

// Of is the product's stewards, the most recently opened first.
func Of(product string) []*Steward {
	var out []*Steward
	for _, s := range All() {
		if Key(s.Product) == Key(product) {
			out = append(out, s)
		}
	}
	return out
}

// Find is the steward with this id, or one whose id starts with it.
func Find(id string) (*Steward, error) {
	var hits []*Steward
	for _, s := range All() {
		if s.ID == id {
			return s, nil
		}
		if id != "" && strings.HasPrefix(s.ID, id) {
			hits = append(hits, s)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return nil, fmt.Errorf("no steward %q", id)
	}
	return nil, fmt.Errorf("%q names %d stewards — use more of the id", id, len(hits))
}

// Current is the product's steward that has not been ended, or nil. There is at
// most one: Start and Reopen end whichever was current before.
func Current(product string) *Steward {
	for _, s := range Of(product) {
		if s.EndedAt == nil {
			return s
		}
	}
	return nil
}

// StateOf is what s is right now. A pane whose claude has exited is not live —
// the shell it dropped to cannot steward anything — while a pane we cannot see
// into is taken at its word.
func StateOf(s *Steward) State {
	if s.EndedAt != nil {
		return Ended
	}
	if running(s.Product) {
		return Live
	}
	return Interrupted
}

func running(product string) bool {
	if !hasSession(session(product)) {
		return false
	}
	idle, known := sessionIdle(session(product))
	return !idle || !known
}

func save(s *Steward) error {
	if err := os.MkdirAll(s.Dir(), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path())
}

// ErrRunning is the product's steward already being live — not a failure; the
// caller attaches to it.
var ErrRunning = errors.New("the steward is already running")

// ErrUntrusted is a steward that started but could not be marked trusted, so it
// is sitting on Claude Code's trust dialog until someone attaches.
var ErrUntrusted = errors.New("started, but its folder could not be marked trusted — attach and accept the trust prompt once")

// Start begins a new steward conversation for product. A live one is left alone
// and returned with ErrRunning; an interrupted one is ended — this is the human
// choosing a fresh start over reopening it — and stays reopenable.
func Start(product string) (*Steward, error) {
	product = Key(product)
	if cfg, err := loadConfig(); err == nil {
		if err := CheckProduct(cfg, product); err != nil {
			return nil, err
		}
	}
	if !available() {
		return nil, errors.New(supervisor.Backend() + " is not available")
	}
	if cur := Current(product); cur != nil {
		if StateOf(cur) == Live {
			return cur, ErrRunning
		}
		if err := end(cur); err != nil {
			return nil, err
		}
	}
	t := now()
	s := &Steward{ID: state.NewID(), Product: product, SessionID: newUUID(), CreatedAt: t, OpenedAt: t}
	err := launch(s, false)
	if err != nil && !errors.Is(err, ErrUntrusted) {
		// A conversation that never started is not one to list or reopen: its
		// folder holds a brief and nothing else, so it goes.
		_ = os.RemoveAll(s.Dir())
		return nil, err
	}
	return s, err
}

// Reopen takes an older steward conversation back up, where it left off: the
// same session resumed in its own folder, its brief rewritten first so an
// upgrade still reaches it. The product's current steward, if it is another,
// is ended — unless it is live, which the human ends first, since ending a
// conversation somebody may be in the middle of is theirs to do.
func Reopen(id string) (*Steward, error) {
	s, err := Find(id)
	if err != nil {
		return nil, err
	}
	if !available() {
		return nil, errors.New(supervisor.Backend() + " is not available")
	}
	cur := Current(s.Product)
	if cur != nil && cur.ID == s.ID && StateOf(s) == Live {
		return s, ErrRunning
	}
	if cur != nil && cur.ID != s.ID {
		if StateOf(cur) == Live {
			return nil, fmt.Errorf("%s's steward %s is live — end it first (one steward per product)", Key(s.Product), cur.ID)
		}
		if err := end(cur); err != nil {
			return nil, err
		}
	}
	// A session left behind by an exited claude is a shell; the reopened one
	// takes its name.
	if hasSession(session(s.Product)) {
		if err := killSession(session(s.Product)); err != nil {
			return nil, err
		}
	}
	s.EndedAt = nil
	s.OpenedAt = now()
	err = launch(s, true)
	if err != nil && !errors.Is(err, ErrUntrusted) {
		return nil, err
	}
	return s, err
}

// End puts a steward down: its session ended if it is the live one, and the
// record stamped so it is no longer the product's current steward. It stays
// reopenable.
func End(id string) (*Steward, error) {
	s, err := Find(id)
	if err != nil {
		return nil, err
	}
	if s.EndedAt != nil {
		return s, fmt.Errorf("steward %s has already ended", s.ID)
	}
	return s, end(s)
}

// EndProduct ends the product's current steward, whichever it is.
func EndProduct(product string) (*Steward, error) {
	cur := Current(product)
	if cur == nil {
		return nil, fmt.Errorf("%s has no steward", Key(product))
	}
	return cur, end(cur)
}

func end(s *Steward) error {
	if Current(s.Product) != nil && Current(s.Product).ID == s.ID && hasSession(session(s.Product)) {
		if err := killSession(session(s.Product)); err != nil {
			return err
		}
	}
	t := now()
	s.EndedAt = &t
	return save(s)
}

// Openings are the steward's first message, new and reopened. The brief itself
// is CLAUDE.md in its folder, which claude loads on every start and keeps
// through compaction.
func opening(s *Steward, resume bool) string {
	if resume {
		return "You have been reopened. Your CLAUDE.md may have changed since — read it again, then the rulings and the open questions, and carry on stewarding " + Key(s.Product) + "."
	}
	return "Start stewarding " + Key(s.Product) + ", as your CLAUDE.md describes."
}

// launch writes the steward's folder fresh — its brief and settings follow the
// binary and the config, so an upgrade changes how it works on its next start —
// trusts it, starts the session and saves the record.
func launch(s *Steward, resume bool) error {
	exe, err := binary()
	if err != nil {
		return err
	}
	if err := write(s, exe, factsFor(s.Product)); err != nil {
		return err
	}
	trusted := trustDir(s.Dir())
	cmd := dispatch.StewardCommand(os.Getenv("CLAUDE_DISPATCHER_STATE"), dispatch.StewardLaunch{
		SessionID: s.SessionID, Resume: resume, Name: PeerName(s.Product), Opening: opening(s, resume),
	})
	if err := newSession(session(s.Product), s.Dir(), cmd, ""); err != nil {
		return err
	}
	if err := save(s); err != nil {
		return err
	}
	if !trusted {
		// Not fatal: the session is up, and the trust dialog is one keypress
		// on attach. Saying so beats a steward that looks idle.
		return ErrUntrusted
	}
	return nil
}

// Facts is what a steward's brief is written from: the product and where its
// work lives, read when the steward starts.
type Facts struct {
	Product string
	Repos   []RepoFacts
	// Recap is the keyword that brings the saved questions back, and
	// AwayAfter how long a question waits before the human counts as away
	// (config's recap_keyword and steward_away_after).
	Recap     string
	AwayAfter time.Duration
}

// RepoFacts is one of the product's repos as the brief names it.
type RepoFacts struct{ Name, Path string }

func factsFor(product string) Facts {
	cfg, err := loadConfig()
	if err != nil {
		cfg = nil
	}
	f := Facts{Product: Key(product), Recap: cfg.Recap(), AwayAfter: cfg.AwayAfter()}
	if cfg == nil {
		return f
	}
	for _, r := range discover(cfg) {
		if Key(r.Product) == f.Product {
			f.Repos = append(f.Repos, RepoFacts{Name: r.Name, Path: r.Path})
		}
	}
	sort.Slice(f.Repos, func(i, j int) bool { return f.Repos[i].Name < f.Repos[j].Name })
	return f
}

// binary is the dispatcher's own path, which the brief and the permission
// rules name: a steward calling whatever `claude-dispatcher` PATH resolves to
// could be driving a different build from the one that started it.
func binary() (string, error) {
	exe, err := executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if strings.Contains(exe, "go-build") {
		return "", errors.New("running via `go run` — install a real binary first (make install); the steward would be pointed at a temporary build")
	}
	return exe, nil
}

// write lays down CLAUDE.md (the brief) and .claude/settings.json (what the
// steward may run without asking, what it must ask for, and what it may never
// do).
func write(s *Steward, exe string, f Facts) error {
	dir := s.Dir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(Brief(exe, f)), 0o644); err != nil {
		return err
	}
	b, err := json.MarshalIndent(Settings(exe), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), append(b, '\n'), 0o644)
}

// Settings is the steward's project settings: its own verbs and the read-only
// forge and git reads it gathers facts with are allowed outright, so it never
// stops on a permission prompt nobody is watching; editing anything is denied,
// because a lead that starts writing code is a second dispatcher nobody
// dispatched.
//
// Recording a ruling is the one thing it must always ask for ("ask" wins over
// any mode): a ruling is the human's decision, and asking is what makes the
// human confirm each one, so the steward cannot grant itself the merges a
// ruling allows. `merge` itself is allowed, because the verb checks the ruling.
func Settings(exe string) map[string]any {
	allow := []string{"Read", "Grep", "Glob"}
	for _, verb := range stewardVerbs {
		allow = append(allow, "Bash("+exe+" "+verb+":*)")
	}
	for _, read := range []string{
		"gh pr view", "gh pr checks", "gh pr diff", "gh pr list",
		"gh run list", "gh run view", "git log", "git diff", "git status", "git show",
	} {
		allow = append(allow, "Bash("+read+":*)")
	}
	return map[string]any{
		"permissions": map[string]any{
			"allow": allow,
			"ask":   []string{"Bash(" + exe + " ruling add:*)"},
			// Merging straight through gh would step round the ruling the merge
			// verb checks, so it is denied here; the verb runs gh itself, which
			// no rule of claude's reaches.
			"deny": []string{"Edit", "Write", "NotebookEdit", "Bash(gh pr merge:*)", "Bash(git push:*)"},
		},
	}
}

// stewardVerbs are the fleet verbs a steward runs without asking.
var stewardVerbs = []string{"status", "reply", "note", "park", "unpark", "ruling list", "ruling end", "merge", "ask", "questions", "answer"}

// newUUID is a random (version 4) UUID, the form claude's --session-id takes.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
