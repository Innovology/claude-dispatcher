package cockpit

// collect_backlog_sources_test.go covers collectBacklog's own wiring: that a
// Linear read and an Azure Boards query come back as backlog rows. Both were
// reached before only by accident — a settings test leaked credentials into
// the environment, and on a CI runner with az installed later tests ran a real
// query against them (see isolateIntegrationEnv). Here each source is stood in
// for on purpose: Linear through the linearAssigned seam, Azure with an `az`
// on PATH that answers the way the real one does.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/linear"
)

// standInAz puts an `az` on PATH that prints out, and nothing else on PATH:
// with no gh to find, the GitHub search reports nothing searched rather than
// reaching the network.
func standInAz(t *testing.T, out string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in az is a shell script")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\nprintf '%s' '" + out + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "az"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestCollectBacklogTakesLinearAndAzureRows(t *testing.T) {
	isolateIntegrationEnv(t)
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	standInAz(t, `[{"id":42,"fields":{"System.Title":"Fix the export","System.State":"Active",`+
		`"Microsoft.VSTS.Common.Priority":1,"System.AreaPath":"Shop"}}]`)
	t.Setenv("AZURE_DEVOPS_ORG", "https://dev.azure.com/acme")
	t.Setenv("AZURE_DEVOPS_PROJECT", "shop")
	t.Setenv("LINEAR_API_KEY", "lin_unscoped")

	prev := linearAssigned
	t.Cleanup(func() { linearAssigned = prev })
	var asked []string
	linearAssigned = func(key string) ([]linear.Issue, error) {
		asked = append(asked, key)
		if key != "lin_unscoped" {
			return nil, errors.New("unknown token")
		}
		return []linear.Issue{{ID: "uuid-7", Identifier: "ENG-7", Title: "Retry webhooks", Priority: "High"}}, nil
	}

	var s snapshot
	collectBacklog(&collectCtx{cfg: &config.Config{}}, &s)

	if len(asked) != 1 || asked[0] != "lin_unscoped" {
		t.Errorf("Linear was read with %v, want the unscoped key once", asked)
	}
	byID := map[string]ticket{}
	for _, tk := range s.backlogTickets {
		byID[tk.id] = tk
	}
	if lin, ok := byID["ENG-7"]; !ok || lin.src != "lin" || lin.title != "Retry webhooks" {
		t.Errorf("the Linear issue should be a row: %+v (all: %+v)", lin, s.backlogTickets)
	}
	ado, ok := byID["AB#42"]
	if !ok {
		t.Fatalf("the Azure work item should be a row: %+v", s.backlogTickets)
	}
	if ado.src != "ado" || ado.title != "Fix the export" || ado.labels != "Active" || ado.pri != blkNormalizePri("urgent") {
		t.Errorf("Azure row = %+v", ado)
	}
	if ado.prompt == "" {
		t.Error("an Azure row should carry the prompt it would dispatch with")
	}
}

// A Linear read that fails, and an az that answers with something that is not
// a work-item list, leave the rest of the backlog standing and add nothing.
func TestCollectBacklogSurvivesFailedSources(t *testing.T) {
	isolateIntegrationEnv(t)
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	standInAz(t, `not json`)
	t.Setenv("AZURE_DEVOPS_ORG", "https://dev.azure.com/acme")
	t.Setenv("AZURE_DEVOPS_PROJECT", "shop")
	t.Setenv("LINEAR_API_KEY", "lin_revoked")

	prev := linearAssigned
	t.Cleanup(func() { linearAssigned = prev })
	linearAssigned = func(string) ([]linear.Issue, error) { return nil, errors.New("401") }

	var s snapshot
	collectBacklog(&collectCtx{cfg: &config.Config{}}, &s)
	for _, tk := range s.backlogTickets {
		if tk.src == "lin" || tk.src == "ado" {
			t.Errorf("a failed source produced a row: %+v", tk)
		}
	}
}
