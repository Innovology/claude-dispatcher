package fleetcmd

// work.go is the two verbs a branchless conversation turns into work with
// (dispatch/work.go): `worktree`, to become a worker on a branch of its own,
// and `dispatch`, to send work out as an orchestrator. Both are run from inside
// the conversation's own session, which is why they find their dispatcher by
// CLAUDE_DISPATCHER_ID — the variable its launch line set — rather than asking.

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// discover is the seam the verbs find repos through; tests hand it a
// portfolio rather than a config and a disk to scan.
var discover = func() ([]repos.Repo, *config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	return repos.Discover(cfg), cfg, nil
}

// self is the dispatcher the verb is run on behalf of: --id where given, else
// the session's own, from the variable its launch line set.
func self(id string) (*state.Dispatch, error) {
	if id == "" {
		id = os.Getenv("CLAUDE_DISPATCHER_ID")
	}
	if id == "" {
		return nil, errors.New("not run from a dispatcher's session (no CLAUDE_DISPATCHER_ID) — pass --id")
	}
	return find(id)
}

func runWorktree(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("worktree", flag.ContinueOnError)
	repoName := fs.String("repo", "", "the repo to work in — one of this conversation's")
	root := fs.String("root", "", "cut a new branch from this branch instead of the repo's root")
	id := fs.String("id", "", "the dispatcher (default: this session's)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	name := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if name == "" {
		return errors.New("usage: worktree --repo <repo> [--root <branch>] <branch or name of the work>")
	}
	d, err := self(*id)
	if err != nil {
		return err
	}
	all, _, err := discover()
	if err != nil {
		return err
	}
	// The repos it may take a branch in are the ones it was dispatched over.
	product, focus := d.Product, ""
	if product == "" {
		focus = d.RepoName
	}
	scope, err := dispatch.ScopeOf(all, product, focus)
	if err != nil {
		return err
	}
	r, err := pickRepo(scope.Repos, *repoName)
	if err != nil {
		return err
	}
	path, err := dispatch.TakeBranch(d, r, name, dispatch.Root(*root))
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "working on %s in %s\n", d.Branch, path)
	_, _ = fmt.Fprintf(out, "cd there: the cockpit tracks %s as %s's from now on. Commit as you go, open the PR, and fix your own CI failures.\n",
		d.Branch, quote(d.Feature))
	return nil
}

// pickRepo is the repo named among rs, or the only one when nothing is named.
func pickRepo(rs []repos.Repo, name string) (repos.Repo, error) {
	if name == "" {
		if len(rs) == 1 {
			return rs[0], nil
		}
		return repos.Repo{}, fmt.Errorf("name the repo with --repo: %s", repoNames(rs))
	}
	for _, r := range rs {
		if r.Name == name {
			return r, nil
		}
	}
	return repos.Repo{}, fmt.Errorf("%s is not one of this conversation's repos: %s", name, repoNames(rs))
}

func repoNames(rs []repos.Repo) string {
	names := make([]string, len(rs))
	for i, r := range rs {
		names[i] = r.Name
	}
	return strings.Join(names, ", ")
}
