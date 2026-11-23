package fleetcmd

// send.go is `dispatch`: a worker sent out from inside a conversation turned
// orchestrator — or from any shell — as an ordinary dispatcher of its own.

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"claude-dispatcher/internal/account"
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
	acct := account.Account{Name: account.Default}
	if p != nil && p.Account != "" && cfg != nil {
		if a, ok := account.Find(cfg, p.Account); ok {
			acct = a
		}
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
