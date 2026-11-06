package dispatch

// branchless.go launches a dispatch that cuts no branch: a conversation about a
// product, or about one repo in it, that can follow the talk to any of the
// product's other repos (see branching.go for why).
//
// Nothing is cut. There is no branch and no worktree; the session reads the
// checkouts the human already has — each repo's canonical one, the checkout its
// row on the products lens reads — and they are handed to claude with
// --add-dir, which is what lets a session reach a directory that is not its
// own without stopping to ask. The session itself runs in a folder of ours,
// under the state dir, for three reasons. Its CLAUDE.md is the brief, and
// claude loads it itself and keeps it through compaction, which a first
// message does not survive: a long conversation still knows, an hour in, that
// "the calendar sync" is in pp-calendar-sync and where that is. A conversation
// about a product has no one checkout it belongs in. And the transcript is
// keyed by the folder a session runs in, so a folder of its own is what lets
// Resume find this conversation again rather than whichever one last ran in a
// shared checkout.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// Scope is what a branchless dispatch spans.
type Scope struct {
	// Product is the product the session spans. "" for a repo the config files
	// under no product, whose scope is then that repo alone.
	Product string
	// Repos are the repositories the session may reach, each through its
	// canonical checkout, the focus first.
	Repos []repos.Repo
	// Focus is the repo the conversation starts on, or "" for the product as a
	// whole.
	Focus string
}

// ScopeOf is the scope for a product, or for one repo in it: every repo the
// portfolio files under that product, the focus first and the rest by name.
// focus names a repo and decides the product; with no focus, product does.
func ScopeOf(all []repos.Repo, product, focus string) (Scope, error) {
	var at *repos.Repo
	if focus != "" {
		for i := range all {
			if all[i].Name == focus {
				at = &all[i]
				break
			}
		}
		if at == nil {
			return Scope{}, fmt.Errorf("repo not found: %s", focus)
		}
		product = at.Product
	}
	if product == "" {
		if at == nil {
			return Scope{}, fmt.Errorf("no product or repo to talk about")
		}
		return Scope{Repos: []repos.Repo{*at}, Focus: at.Name}, nil
	}
	s := Scope{Product: product, Focus: focus}
	var rest []repos.Repo
	for _, r := range all {
		switch {
		case r.Product != product:
		case at != nil && r.Name == at.Name:
			s.Repos = append(s.Repos, r)
		default:
			rest = append(rest, r)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].Name < rest[j].Name })
	s.Repos = append(s.Repos, rest...)
	if len(s.Repos) == 0 {
		return Scope{}, fmt.Errorf("no repo in %s has been found under the scan roots", product)
	}
	return s, nil
}

// focusRepo is the repo the conversation starts on, or nil for the product as
// a whole.
func (s Scope) focusRepo() *repos.Repo {
	if s.Focus == "" {
		return nil
	}
	for i := range s.Repos {
		if s.Repos[i].Name == s.Focus {
			return &s.Repos[i]
		}
	}
	return nil
}

// label is what the audit and the refusals call the scope: the repo it starts
// on, else the product.
func (s Scope) label() string {
	if s.Focus != "" {
		return s.Focus
	}
	return s.Product
}

// dirs are what the session is given: each repo's canonical checkout, to read,
// and the folder its dispatch worktrees are cut in (worktreeHome), so that a
// checkout the conversation cuts when it turns into work (TakeBranch) is one it
// can work in without stopping to ask. A directory inside another one given is
// left out — on a project layout the repo's folder holds its canonical checkout
// — and so is a repeat, the focus first and the rest in scope order.
func (s Scope) dirs() []string {
	var cand []string
	for _, r := range s.Repos {
		if r.Path != "" {
			cand = append(cand, r.Path)
		}
		if r.Name != "" {
			cand = append(cand, worktreeHome(r))
		}
	}
	var out []string
	for i, c := range cand {
		covered := false
		for j, o := range cand {
			if i != j && within(o, c) && (!samePath(o, c) || j < i) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, c)
		}
	}
	return out
}

// worktreeHome is the folder a repo's dispatch worktrees are cut in: beside the
// repo's own checkouts where its layout has a place for them, else this tool's
// own worktrees/<repo> (see worktreePath).
func worktreeHome(r repos.Repo) string {
	if r.WorktreeRoot != "" {
		return r.WorktreeRoot
	}
	return filepath.Join(state.WorktreesDir(), r.Name)
}

// LaunchBranchless starts a session over s: no branch, no worktree, a folder of
// its own under the state dir holding the brief (BranchlessBrief), and every
// repo's checkout given with --add-dir. Everything Launch refuses, it refuses
// too — a name already live, an account that would leave the session sitting
// there, a prompt past the ceiling — and for the same reasons.
//
// The session's server, environment and shell are the focus repo's, so a
// conversation that starts on a repo runs with that repo's toolchain on PATH
// (repos.EnvIn names the checkout's flake, since the folder has none). A
// conversation about the product as a whole has no repo to borrow them from: it
// runs in the plain environment, on a server named for the product, the way a
// repo's sessions are on one named for the repo. Not on "no server": with no -L
// tmux answers on whichever server $TMUX points at, so a session given none
// lands on the cockpit's own server when the cockpit runs inside tmux, and is
// looked for on the default one by a cockpit started anywhere else.
func LaunchBranchless(s Scope, feature, prompt string, mode Mode, model Model, fanOut bool, acct account.Account) (d *state.Dispatch, err error) {
	audit(state.Event{Event: state.EventDispatchAsked, Feature: feature, Repo: s.label()})
	defer func() {
		switch {
		case err != nil:
			audit(state.Event{Event: state.EventDispatchFailed, Feature: feature, Repo: s.label(),
				Reason: err.Error(), DispatcherID: idOf(d)})
		case d != nil:
			audit(state.Event{Event: state.EventDispatchLaunched, Feature: feature, Repo: s.label(),
				DispatcherID: d.ID, Cwd: d.WorktreePath})
		}
	}()

	slug, err := preflight(feature, prompt, acct)
	if err != nil {
		return nil, err
	}
	dirs := s.dirs()
	if len(dirs) == 0 {
		return nil, fmt.Errorf("%s has no checkout to read", s.label())
	}
	// Our own worktrees folder may not exist yet for a repo nothing has been
	// dispatched into, and claude is given it all the same.
	for _, dir := range dirs {
		if within(state.WorktreesDir(), dir) {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
	}
	// The folder is the record's own: the slug for the human reading the
	// directory, the id so that two conversations whose names share their first
	// five words never share a brief or a transcript key. A worktree is reused
	// by a re-dispatch on purpose — it may hold the work — and a folder holding
	// only a brief has nothing to carry over, so a new conversation gets a new
	// one.
	id := state.NewID()
	home := filepath.Join(state.BranchlessDir(), slug+"-"+id)
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, err
	}
	brief := BranchlessBrief(s, feature)
	if err := os.WriteFile(filepath.Join(home, BriefFile), []byte(brief), 0o644); err != nil {
		return nil, fmt.Errorf("could not write the brief: %w", err)
	}
	// And a copy beside the prompt, which nothing removes, for Resume to put
	// back if the folder goes (restoreBrief).
	if _, err := state.WritePrompt(briefName(id), brief); err != nil {
		return nil, fmt.Errorf("could not keep a copy of the brief: %w", err)
	}
	// A folder we made and wrote, which nobody else has to vouch for (see
	// TrustOwnDir) — and in the account the session runs under, since trust is
	// per config directory.
	trusted := trustOwnDir(acct.Env(), home)

	mode, model = mode.Normalize(), model.Normalize()
	prompt = strings.TrimRight(withFanOut(withContract(prompt, mode, BranchNone), fanOut), "\n")

	socket := s.Product
	var env, shell, repoName, repoPath string
	if r := s.focusRepo(); r != nil {
		socket, shell, repoName, repoPath = r.Socket, r.Shell, r.Name, r.Path
		env = repos.EnvIn(r.Env, r.Path, home)
	}
	d = &state.Dispatch{
		ID:           id,
		Feature:      feature,
		Slug:         slug,
		RepoPath:     repoPath,
		RepoName:     repoName,
		Product:      s.Product,
		Branchless:   true,
		AddDirs:      dirs,
		WorktreePath: home,
		Prompt:       prompt,
		Mode:         string(mode),
		Model:        string(model),
		FanOut:       fanOut,
		TmuxSocket:   socket,
		EnvCommand:   env,
		PaneShell:    shell,
		Account:      accountName(acct),
		ConfigDir:    acct.Env(),
		TmuxSession:  uniqueName(supervisor.Session{Name: "disp-" + slug, Socket: socket}),
		Status:       state.StatusLaunching,
		StatusReason: launchReason(trusted),
		CreatedAt:    time.Now(),
	}
	if err := state.Save(d); err != nil {
		return nil, err
	}
	return d, start(d, prompt, home, mode, model)
}

// BranchlessBrief is the CLAUDE.md a branchless session runs under: what it
// spans, where each repo lives and what its work merges into, where it starts,
// and how to treat the checkouts it was given. It is the brief rather than the
// prompt because it has to last the whole conversation, and a conversation
// that moves between repos is exactly the one that needs to look this up again.
func BranchlessBrief(s Scope, feature string) string {
	var b strings.Builder
	what := "the product **" + s.Product + "**"
	if s.Product == "" {
		what = "the repository **" + s.Focus + "**"
	}
	fmt.Fprintf(&b, "# %s — a conversation with no branch\n\n", feature)
	fmt.Fprintf(&b, "claude-dispatcher started this session with no branch and no checkout of its own. "+
		"It spans %s: every repository below, each through the human's own checkout, "+
		"which this session was given with --add-dir.\n\n", what)
	switch {
	case s.Focus != "" && len(s.Repos) > 1:
		fmt.Fprintf(&b, "The conversation starts on **%s**, and may go to any of the others.\n\n", s.Focus)
	case s.Focus == "":
		b.WriteString("The conversation is about the product as a whole.\n\n")
	}
	b.WriteString("| repository | checkout | work merges into |\n| --- | --- | --- |\n")
	for _, r := range s.Repos {
		into := r.RootBranch
		if into == "" {
			into = "origin's default branch"
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s |\n", r.Name, r.Path, into)
	}
	cmd := selfCommand()
	b.WriteString("\n" +
		"- Each repository keeps its own conventions in its CLAUDE.md and README. Read a repository's " +
		"before answering about it, and again when the conversation moves to another one.\n" +
		"- `git` and `gh` answer from inside a checkout (`git -C <checkout> …`, or `cd` there for `gh`) " +
		"and see every branch and pull request, so nothing needs checking out to be read.\n" +
		"- These checkouts are shared with the human and with any session they run there. Read them; " +
		"do not edit, commit, or switch branches in them.\n")
	fmt.Fprintf(&b, "\n## When the conversation turns into work\n\n"+
		"It can, and work goes on a branch of its own, one of two ways.\n\n"+
		"- **Do it here.** `%[1]s worktree --repo <repo> <name>` moves this conversation onto a branch: "+
		"a new one cut for `<name>` the way every dispatch's is (`--root <branch>` to cut it from another "+
		"branch), or `<name>` itself when that branch already exists. It prints the checkout: `cd` there "+
		"and carry the work through as any dispatch would — commit as you go, open the PR, fix your own CI "+
		"failures. The cockpit tracks the branch as this conversation's from then on; a conversation takes one branch, and sends any further work out.\n"+
		"- **Send it out.** `%[1]s dispatch --repo <repo> --title \"<a few words>\" \"<the brief>\"` starts a "+
		"worker of its own, in its own worktree, on the human's table. Send one per independent piece. "+
		"`%[1]s status --json` shows them (their `dispatched_by` is this conversation), and "+
		"`%[1]s reply <id> \"<one line>\"` answers one that is waiting.\n\n"+
		"The folders a repo's worktrees are cut in are among the directories this session was given, so a "+
		"checkout cut with `worktree` can be worked in without asking.\n", cmd)
	return b.String()
}

// selfCommand is how a session started by this build should call it back: the
// plain name when PATH's claude-dispatcher is this very binary — the durable
// name, which survives an upgrade — and this binary's own path otherwise, since
// a second, older copy on PATH would be driving another build.
var selfCommand = func() string {
	exe, err := os.Executable()
	if err != nil {
		return "claude-dispatcher"
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if found, err := exec.LookPath("claude-dispatcher"); err == nil {
		if r, err := filepath.EvalSymlinks(found); err == nil {
			// The Nix package's makeWrapper script sits in front of the binary,
			// which it moves to .claude-dispatcher-wrapped beside itself.
			if r == exe || (filepath.Dir(r) == filepath.Dir(exe) && filepath.Base(exe) == "."+filepath.Base(r)+"-wrapped") {
				return "claude-dispatcher"
			}
		}
	}
	return exe
}

// BriefFile is the brief's name in a branchless dispatch's folder: claude's own
// project memory file, which it loads on start and reloads after compaction.
const BriefFile = "CLAUDE.md"

// briefName is the prompts-dir name the copy of a record's brief is kept under.
func briefName(id string) string { return id + "-brief" }

// restoreBrief puts a branchless dispatcher's brief back in dir when it is not
// there, from the copy its launch kept. The contract it was launched with points
// at "the repositories CLAUDE.md lists", and a folder with no CLAUDE.md lists
// none. A record from before the copy was kept has none to restore, and resumes
// as it is.
func restoreBrief(id, dir string) {
	path := filepath.Join(dir, BriefFile)
	if _, err := os.Stat(path); err == nil {
		return
	}
	if brief, err := os.ReadFile(state.PromptPath(briefName(id))); err == nil {
		_ = os.WriteFile(path, brief, 0o644)
	}
}

// trustOwnDir is the seam a branchless launch marks its folder trusted
// through. Unlike InheritTrustFor it always writes — there is no repo whose
// trust it waits on — so a test must not reach the human's own .claude.json.
var trustOwnDir = TrustOwnDirFor
