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
	}
	all, cfg, err := discover()
	if err != nil {
		return err
	}
	r, err := pickRepo(all, *repoName)
	if err != nil {
		return fmt.Errorf("no repo %s", *repoName)
	}
	// Sent from a dispatcher's session, the worker is that dispatcher's, and
	// runs on the subscription it runs on; sent from a plain shell, it is the
	// human's, on their own login.
	parent := os.Getenv("CLAUDE_DISPATCHER_ID")
	acct := account.Account{Name: account.Default}
	if parent != "" {
		if p, err := find(parent); err == nil && p.Account != "" && cfg != nil {
			if a, ok := account.Find(cfg, p.Account); ok {
				acct = a
			}
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
