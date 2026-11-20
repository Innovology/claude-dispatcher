package dispatch

// handoff.go is a dispatcher handing its work on to another repo of its
// product (ADR 0034): what it built here is something a repo beside it has to
// consume, so it starts a dispatch there, and its own run is over.

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// andList joins names the way a sentence lists them.
func andList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// HandoffTarget is the repo name names among all, refused unless it is
// another repo of from's product.
//
// The product is the one config gives from's repo now, not the one on the
// record: a repo moved to another product since its dispatch launched belongs
// where the human has since put it. A record whose repo discovery no longer
// finds keeps the product it launched under.
func HandoffTarget(from *state.Dispatch, all []repos.Repo, name string) (repos.Repo, error) {
	product := from.Product
	for _, r := range all {
		if r.Name == from.RepoName {
			product = r.Product
			break
		}
	}
	if product == "" {
		return repos.Repo{}, fmt.Errorf("%s is in no product, so there is no repo beside it to hand off to — put it in one with the cockpit's assignment editor first", from.RepoName)
	}
	var siblings []repos.Repo
	for _, r := range all {
		if r.Product == product && r.Name != from.RepoName {
			siblings = append(siblings, r)
		}
	}
	names := make([]string, len(siblings))
	for i, r := range siblings {
		names[i] = r.Name
	}
	switch {
	case len(siblings) == 0:
		return repos.Repo{}, fmt.Errorf("%s is the only repo of %s that can be found, so there is nowhere to hand off to", from.RepoName, product)
	case name == "":
		return repos.Repo{}, fmt.Errorf("name the repo with --repo: %s", andList(names))
	case name == from.RepoName:
		return repos.Repo{}, fmt.Errorf("%s is this dispatcher's own repo — a handoff goes to another repo of %s: %s", name, product, andList(names))
	}
	for _, r := range siblings {
		if r.Name == name {
			return r, nil
		}
	}
	return repos.Repo{}, fmt.Errorf("%s is not a repo of %s — hand off to %s", name, product, andList(names))
}

// Handoff dispatches title in to, with brief, as the work from hands on; then
// links the two and ends from's run.
//
// The new dispatcher is told where the work it builds on lives — the
// dispatcher, its repo, branch, pull request and checkout — because that is
// what it has to consume and the brief, written by a session in the middle of
// its own work, may well take it for granted.
//
// from's run ends only once the launch has worked: a handoff that did not
// happen leaves the work with the dispatcher that has it. The new record is
// linked either way, since a launch that failed is kept as a row (ADR 0009)
// and where it came from is part of what it reports. It ends exited, the word
// for a run that is over, and not done, which is "live" — the work it handed
// on from is shipped when its own pull request is, and the tracker still says
// so; a dispatcher already live keeps done, because a handoff takes nothing
// back. It is not dismissed: nobody watched it end, so it holds its row with
// what it handed off to until the human takes it off (ADR 0012).
func Handoff(from *state.Dispatch, to repos.Repo, title, brief string, mode Mode, model Model, root Root, acct account.Account) (*state.Dispatch, error) {
	title, brief = strings.TrimSpace(title), strings.TrimSpace(brief)
	switch {
	case from == nil:
		return nil, errors.New("no dispatcher to hand off from")
	case title == "":
		return nil, errors.New("give the work a title — a few words, the feature's name in the cockpit")
	case brief == "":
		return nil, errors.New("say what it should do — the title alone is not a brief")
	case Slugify(title) == from.Slug:
		// The feature name is the key: the cockpit maps one to a record, and a
		// second live dispatcher of it would be refused anyway, since this one
		// is live until the launch has worked.
		return nil, fmt.Errorf("%q is this dispatcher's own name — give the work in %s a name of its own", from.Feature, to.Name)
	}
	d, err := launchHandoff(to, title, handoffPrompt(from, title, brief), mode, model, root, false, acct)
	if d != nil {
		if lerr := linkHandoff(from.ID, d.ID, err == nil, ReasonHandedOffTo(title, to.Name)); lerr != nil && err == nil {
			err = lerr
		}
	}
	return d, err
}

// launchHandoff is Launch, as a seam.
var launchHandoff = Launch

// ReasonHandedOffTo is the reason a handoff ends its dispatcher's run with:
// state.ReasonHandedOff, and what it handed off to.
func ReasonHandedOffTo(title, repo string) string {
	return state.ReasonHandedOff + fmt.Sprintf("%q in %s", title, repo)
}

// handoffPrompt is the new dispatcher's prompt: the title and brief as any
// dispatch's, then where the work it builds on is.
func handoffPrompt(from *state.Dispatch, title, brief string) string {
	where := []string{from.RepoName}
	if from.Branch != "" {
		where = append(where, "branch "+from.Branch)
	}
	if from.PRURL != "" {
		where = append(where, from.PRURL)
	}
	line := fmt.Sprintf("Handed off by %q, a dispatcher in %s, whose work this builds on.", from.Feature, strings.Join(where, ", "))
	if from.WorktreePath != "" {
		line += " Its checkout is " + from.WorktreePath + "."
	}
	return title + "\n\n" + brief + "\n\n" + line
}

// linkHandoff writes both ends of a handoff under the hook lock, against a
// fresh read, for the reason markSessionStarted does: the new session's hooks
// may already have written its record. ended says the launch worked, so
// from's run is over and the link on it is real.
func linkHandoff(fromID, toID string, ended bool, reason string) error {
	release := state.Lock()
	defer release()
	found := 0
	for _, d := range state.LoadAll() {
		switch d.ID {
		case toID:
			d.HandedOffFrom = fromID
		case fromID:
			if !ended {
				found++
				continue
			}
			d.HandedOffTo = append(d.HandedOffTo, toID)
			if !d.Finished() {
				status := state.StatusExited
				if d.DeployedAt != nil {
					status = state.StatusDone
				}
				d.Stop(status, reason, time.Now())
			}
		default:
			continue
		}
		found++
		if err := state.Save(d); err != nil {
			return err
		}
	}
	if found < 2 {
		return fmt.Errorf("could not find both ends of the handoff (%s → %s) to link them", fromID, toID)
	}
	return nil
}

// ScrubSessionEnv removes from this process the variables a Claude Code
// session puts in its tools' environment, before this process starts another
// session. A pane takes its environment from the client that asked for it (ADR
// 0014), and a dispatch started from inside a session would otherwise start as
// that session's child — its session id, its messaging socket, its dispatcher
// id, its account (CLAUDE_CONFIG_DIR, which the launch line sets only when the
// account is not the default). CLAUDE_DISPATCHER_STATE is kept: it names the
// store, which is the same one.
func ScrubSessionEnv() {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if sessionVar(name) {
			_ = os.Unsetenv(name)
		}
	}
}

func sessionVar(name string) bool {
	switch name {
	case "CLAUDECODE", "CLAUDE_PID", "CLAUDE_EFFORT", "CLAUDE_DISPATCHER_ID", "CLAUDE_CONFIG_DIR", "AI_AGENT":
		return true
	}
	return strings.HasPrefix(name, "CLAUDE_CODE_")
}
