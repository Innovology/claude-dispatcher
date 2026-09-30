package fleetcmd

import (
	"bytes"
	"strings"
	"testing"

	"claude-dispatcher/internal/dispatch"
)

// Without --yes the listing ends by saying nothing was touched and how to do
// it; with it, by what was done. Removals lead, keeps close with their reason.
func TestPrintTidy(t *testing.T) {
	items := []dispatch.TidyItem{
		{Path: "/w/keep", Repo: "shop", Feature: "busy", Action: dispatch.TidyKeep, Reason: "its session is still open"},
		{Path: "/w/strip", Repo: "shop", Feature: "wip", Action: dispatch.TidyStrip, Reason: "uncommitted changes", NodeModules: []string{"node_modules", "web/node_modules"}},
		{Path: "/w/gone", Repo: "shop", Feature: "old", Action: dispatch.TidyRemove},
	}
	var out bytes.Buffer
	printTidy(items, false, &out)
	got := out.String()
	lines := strings.Split(got, "\n")
	if !strings.HasPrefix(lines[0], "remove") || !strings.HasPrefix(lines[1], "strip") || !strings.HasPrefix(lines[2], "keep") {
		t.Fatalf("order:\n%s", got)
	}
	for _, want := range []string{"/w/gone", "removes node_modules, web/node_modules", "its session is still open",
		"1 worktree(s) to remove · 1 to strip of node_modules · 1 kept", "nothing has been touched"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}

	out.Reset()
	printTidy(items, true, &out)
	if !strings.Contains(out.String(), "removed 1 worktree(s) · stripped node_modules from 1 · kept 1") {
		t.Errorf("done summary:\n%s", out.String())
	}

	out.Reset()
	printTidy(nil, false, &out)
	if !strings.Contains(out.String(), "nothing to tidy") {
		t.Errorf("empty:\n%s", out.String())
	}
}
