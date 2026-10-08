package fleetcmd

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
)

// runTidy is `tidy`: what a tidy of the finished fleet's worktrees would do,
// and with --yes, doing it (see dispatch.Tidy for what it will and will not
// take). Without --yes it touches no folder, because what it removes is on
// somebody's disk and the list is worth a look first; like every cockpit load,
// it does retire records whose session is provably gone before it reads them.
func runTidy(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("tidy", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "remove them, rather than only saying what would go")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q (tidy [--yes])", fs.Arg(0))
	}
	ds := state.LoadAll()
	// A record still claiming "working" over a session that is gone would keep
	// its folder; the ghost sweep every cockpit load runs settles it first.
	dispatch.ReconcileSessions(ds)
	items := dispatch.PlanTidy(ds)
	if *yes {
		items = dispatch.Tidy(items)
	}
	printTidy(items, *yes, out)
	return nil
}

var tidyOrder = map[dispatch.TidyAction]int{dispatch.TidyRemove: 0, dispatch.TidyStrip: 1, dispatch.TidyKeep: 2}

func printTidy(items []dispatch.TidyItem, done bool, out io.Writer) {
	if len(items) == 0 {
		_, _ = fmt.Fprintln(out, "nothing to tidy: no finished dispatcher has a worktree on disk")
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if tidyOrder[a.Action] != tidyOrder[b.Action] {
			return tidyOrder[a.Action] < tidyOrder[b.Action]
		}
		return a.Path < b.Path
	})
	count := map[dispatch.TidyAction]int{}
	for _, it := range items {
		count[it.Action]++
		detail := it.Path
		switch it.Action {
		case dispatch.TidyStrip:
			detail = it.Reason + " · removes " + strings.Join(it.NodeModules, ", ")
		case dispatch.TidyKeep:
			detail = it.Reason
		}
		if it.Err != "" && it.Action != dispatch.TidyKeep {
			detail += " · failed: " + it.Err
		}
		_, _ = fmt.Fprintf(out, "%-6s  %-40s  %s\n", it.Action, clip(it.Repo+"/"+it.Feature, 40), detail)
	}
	if done {
		_, _ = fmt.Fprintf(out, "\nremoved %d worktree(s) · stripped node_modules from %d · kept %d\n",
			count[dispatch.TidyRemove], count[dispatch.TidyStrip], count[dispatch.TidyKeep])
		return
	}
	_, _ = fmt.Fprintf(out, "\n%d worktree(s) to remove · %d to strip of node_modules · %d kept\n",
		count[dispatch.TidyRemove], count[dispatch.TidyStrip], count[dispatch.TidyKeep])
	if count[dispatch.TidyRemove]+count[dispatch.TidyStrip] > 0 {
		_, _ = fmt.Fprintln(out, "nothing has been touched · claude-dispatcher tidy --yes does it")
	}
}
