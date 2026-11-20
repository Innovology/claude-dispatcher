package fleetcmd

// handoff.go is `handoff`: run from inside a dispatcher's session, it hands that
// dispatcher's work on to another repo of its product (dispatch.Handoff, ADR
// 0034). The session finds itself by the variable its launch line set, so a
// dispatcher never has to know its own id.

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
	"claude-dispatcher/internal/state"
)

// handoff is dispatch.Handoff, as a seam.
var handoff = dispatch.Handoff

// stdin is where a brief of "-" is read from.
var stdin io.Reader = os.Stdin

const handoffUsage = `usage: handoff --repo <repo> --title "<a few words>" [--root <branch>] [--mode auto|manual|plan] [--model <alias>] [--account <name>] [--from <id>] "<what it should do>" (or - to read it from stdin)`

func runHandoff(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("handoff", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // the usage line below says it all, once
	repoName := fs.String("repo", "", "the repo of this product the work goes to")
	title := fs.String("title", "", "a few words: the feature name, its branch and its session")
	root := fs.String("root", "", "cut its branch from this branch instead of the repo's root")
	mode := fs.String("mode", "", "auto, manual or plan (default: this dispatcher's)")
	model := fs.String("model", "", "a model alias claude offers, or default (default: this dispatcher's)")
	acctName := fs.String("account", "", "the subscription it runs on (default: this dispatcher's)")
	from := fs.String("from", "", "the dispatcher handing off, by id or feature (default: this session's)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%v\n%s", err, handoffUsage)
	}
	brief := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if brief == "-" {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		brief = strings.TrimSpace(string(b))
	}
	if strings.TrimSpace(*title) == "" || brief == "" {
		return errors.New(handoffUsage)
	}
	if *mode != "" && !dispatch.Mode(*mode).Known() {
		return fmt.Errorf("no mode %q — auto, manual or plan", *mode)
	}
	if *model != "" && !dispatch.Model(*model).Known() {
		return fmt.Errorf("no model %q here — %s", *model, modelNames())
	}

	src, err := handingOff(*from)
	if err != nil {
		return err
	}
	all, cfg, err := discover()
	if err != nil {
		return err
	}
	to, err := dispatch.HandoffTarget(src, all, *repoName)
	if err != nil {
		return err
	}
	acct, err := handoffAccount(src, cfg, *acctName)
	if err != nil {
		return err
	}
	m, mo := dispatch.Mode(*mode), dispatch.Model(*model)
	if *mode == "" {
		m = dispatch.Mode(src.Mode)
	}
	if *model == "" {
		mo = dispatch.Model(src.Model)
	}

	// What this process carries from the session it runs in, a pane started
	// from it would carry too (ADR 0014): it goes before the launch.
	dispatch.ScrubSessionEnv()
	d, err := handoff(src, to, *title, brief, m, mo, dispatch.Root(*root), acct)
	if err != nil {
		if d != nil {
			return fmt.Errorf("%w — the launch is kept in the cockpit as a row that did not start, and %q is still yours to finish", err, src.Feature)
		}
		return err
	}
	_, _ = fmt.Fprintf(out, "handed off to %s in %s · branch %s · id %s · session %s\n",
		quote(d.Feature), d.RepoName, d.Branch, d.ID, d.TmuxSession)
	if *from == "" {
		_, _ = fmt.Fprintf(out, "%s is finished: the cockpit holds its row as handed off, and %s carries on in %s. End your turn here.\n",
			quote(src.Feature), quote(d.Feature), d.RepoName)
	}
	return nil
}

// handingOff is the dispatcher the handoff is made from: --from where given,
// else the session's own. A dispatched session carries its record's id in
// CLAUDE_DISPATCHER_ID; an adopted one was started before any record and
// carries none, but its record knows its conversation, and Claude Code puts
// that in every tool's environment.
func handingOff(key string) (*state.Dispatch, error) {
	if key != "" {
		return find(key)
	}
	if id := os.Getenv("CLAUDE_DISPATCHER_ID"); id != "" {
		return find(id)
	}
	if sid := os.Getenv("CLAUDE_CODE_SESSION_ID"); sid != "" {
		for _, d := range state.LoadAll() {
			if d.SessionID == sid {
				return d, nil
			}
		}
	}
	return nil, errors.New("not run from a dispatcher's session — run it from inside one, or name the dispatcher with --from <id>")
}

// handoffAccount is the subscription the new dispatcher runs on: the one the
// dispatcher handing off runs on unless --account names another. Which login
// pays is the human's choice, and they made it for this work when they
// dispatched it. Taken by its directory, as Resume takes it, so an account
// renamed in config since still names the login that was chosen.
func handoffAccount(src *state.Dispatch, cfg *config.Config, name string) (account.Account, error) {
	if name == "" {
		a := account.Account{Name: src.Account, Dir: src.ConfigDir}
		if a.IsDefault() {
			a.Name = account.Default
		}
		return a, nil
	}
	a, ok := account.Find(cfg, name)
	if !ok {
		return account.Account{}, fmt.Errorf("no account %q — claude-dispatcher account lists them", name)
	}
	return a, nil
}

// modelNames lists the models this machine's claude offers, for a refusal.
func modelNames() string {
	var names []string
	for _, m := range dispatch.Models() {
		names = append(names, string(m))
	}
	return strings.Join(names, ", ")
}
