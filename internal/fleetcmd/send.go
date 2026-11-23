package fleetcmd

// send.go is `dispatch`: a dispatcher sent out from inside a conversation turned
// orchestrator — or from any shell — as an ordinary dispatcher of its own.

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// launch is dispatch.Launch, as a seam, so a test can read what `dispatch`
// asks for without starting a session.
var launch = dispatch.Launch

func runDispatch(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("dispatch", flag.ContinueOnError)
	repoName := fs.String("repo", "", "the repo the work goes in")
	title := fs.String("title", "", "a few words: the feature name, the branch and the session")
	root := fs.String("root", "", "cut the branch from this branch instead of the repo's root")
	mode := fs.String("mode", string(dispatch.DefaultMode), "auto, manual or plan")
	model := fs.String("model", string(dispatch.DefaultModel), "a model alias claude advertises, or default")
	if err := fs.Parse(args); err != nil {
		return err
	}
	brief := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if brief == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		brief = strings.TrimSpace(string(b))
	}
	switch {
	case *repoName == "" || strings.TrimSpace(*title) == "":
		return errors.New("usage: dispatch --repo <repo> --title \"<a few words>\" [--root <branch>] [--mode auto|manual|plan] \"<the brief>\" (or - to read it from stdin)")
	case brief == "":
		return errors.New("say what it should do — the title alone is not a brief")
	case !dispatch.Mode(*mode).Known():
		// Refused, not normalised: a typo for manual or plan would otherwise
		// send the work out unattended, which is the one mode it was not asked for.
		return fmt.Errorf("--mode %s is not a mode: auto, manual or plan", *mode)
	}
	all, cfg, err := discover()
	if err != nil {
		return err
	}
	// Sent from a dispatcher's session, the work is that dispatcher's: it goes
	// to a repo of the product it was dispatched over, as `worktree` does, and
	// runs on the subscription it runs on. Sent from a plain shell, it is the
	// human's, into any repo, on their own login.
	parent := os.Getenv("CLAUDE_DISPATCHER_ID")
	var p *state.Dispatch
	if parent != "" {
		if p, err = find(parent); err != nil {
			return fmt.Errorf("this session's dispatcher %s: %w", parent, err)
		}
	}
	var r repos.Repo
	if p != nil {
		r, err = pickRepoOf(p, all, *repoName)
	} else if r, err = pickRepo(all, *repoName); err != nil {
		err = fmt.Errorf("no repo %s", *repoName)
	}
	if err != nil {
		return err
	}
	if n := sentOut(parent); n >= MaxSentOut {
		return fmt.Errorf("%s already has %d dispatchers out, the most one may have at once — wait for one to finish, or ask the human", quote(p.Feature), n)
	}
	acct, err := senderAccount(p, cfg, out)
	if err != nil {
		return err
	}
	// What this process carries from the session it runs in, a pane started
	// from it would carry too (ADR 0014): it goes before the launch.
	dispatch.ScrubSessionEnv()
	feature := strings.TrimSpace(*title)
	d, err := launch(r, feature, feature+"\n\n"+brief, dispatch.Mode(*mode), dispatch.Model(*model),
		dispatch.Root(*root), false, acct)
	if err != nil {
		return err
	}
	if err := dispatch.MarkDispatchedBy(d.ID, parent); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "dispatched %s → %s on %s · id %s · session %s\n", quote(d.Feature), d.RepoName, d.Branch, d.ID, d.TmuxSession)
	return nil
}

// senderAccount is the subscription work sent from p runs on: p's own, as
// config names it now, else the directory p's record says it runs under —
// said, because config no longer knowing the name is something the human
// should hear about, and falling back to their own login without a word moved
// the work onto a subscription nobody chose. A plain shell's (no p) is the
// human's own login.
func senderAccount(p *state.Dispatch, cfg *config.Config, out io.Writer) (account.Account, error) {
	if p == nil || p.Account == "" || p.Account == account.Default {
		return account.Account{Name: account.Default}, nil
	}
	if a, ok := account.Find(cfg, p.Account); ok {
		return a, nil
	}
	if p.ConfigDir == "" {
		return account.Account{}, fmt.Errorf("%s runs on the account %q, which config.toml no longer has, and its record names no directory — add the account back, or dispatch from a plain shell",
			quote(p.Feature), p.Account)
	}
	_, _ = fmt.Fprintf(out, "the account %q is not in config.toml any more; sending this on the directory %s runs under, %s\n",
		p.Account, quote(p.Feature), p.ConfigDir)
	return account.Account{Name: p.Account, Dir: p.ConfigDir}, nil
}

// MaxSentOut is how many dispatchers one dispatcher may have out at once. Each
// is a claude session, a worktree and a share of one subscription's window,
// started with nobody asked: a conversation sends one per independent piece
// and rarely needs more, and a session that keeps sending — a loop, a
// misread brief — would otherwise fill the machine and spend the window
// before anyone looked at the table.
const MaxSentOut = 8

// sentOut is how many of the dispatchers parent sent are still going.
func sentOut(parent string) int {
	n := 0
	for _, d := range state.LoadAll() {
		if parent != "" && d.DispatchedBy == parent && !d.Finished() {
			n++
		}
	}
	return n
}
