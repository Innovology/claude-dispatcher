// Package accountcmd is `claude-dispatcher account`: the Claude subscriptions
// a dispatch can run under, listed, added and logged in (internal/account).
//
// Adding one is the whole setup in one command, because each step left undone
// is a dispatch that starts and then sits there: the directory is made and
// named in the config, our hooks and status line are installed in its
// settings (each config directory reads only its own), it is logged in with
// Claude Code's own `claude auth login`, and Claude Code is opened there once
// if its first-run setup has not happened yet.
package accountcmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/initcmd"
	"claude-dispatcher/internal/state"
)

const usage = `claude-dispatcher account                 list subscriptions, who each is, and what is left
claude-dispatcher account add <name> [dir] add one (dir defaults to ~/.claude-<name>) and log it in
claude-dispatcher account login <name>     log an account in again
claude-dispatcher account remove <name>    forget one (its directory and login are left alone)
`

// Run is the subcommand. Output goes to out; prompts and the login itself use
// the terminal, which is the point of `add` — it is interactive by nature.
func Run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "list" {
		return list(out, errOut)
	}
	switch args[0] {
	case "add":
		if len(args) < 2 || len(args) > 3 {
			_, _ = fmt.Fprint(errOut, usage)
			return 2
		}
		dir := ""
		if len(args) == 3 {
			dir = args[2]
		}
		return add(args[1], dir, out, errOut)
	case "login":
		if len(args) != 2 {
			_, _ = fmt.Fprint(errOut, usage)
			return 2
		}
		return login(args[1], out, errOut)
	case "remove", "rm":
		if len(args) != 2 {
			_, _ = fmt.Fprint(errOut, usage)
			return 2
		}
		return remove(args[1], out, errOut)
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(out, usage)
		return 0
	}
	_, _ = fmt.Fprintf(errOut, "account: unknown command %q\n\n%s", args[0], usage)
	return 2
}

func list(out, errOut io.Writer) int {
	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "account: loading config:", err)
		return 1
	}
	now := time.Now()
	for _, a := range account.List(cfg) {
		auth := account.Probe(a)
		who := "—"
		switch {
		case !auth.Known:
			who = "unknown (claude auth status did not answer)"
		case !auth.LoggedIn:
			who = "not logged in"
		case auth.Email != "":
			who = auth.Email
			if auth.Plan != "" {
				who += " · " + auth.Plan
			}
		}
		left := "no reading yet"
		if l, ok := account.ReadLimits(state.Dir(), a.ConfigDir()); ok {
			left = l.Brief(now) + " · " + ago(now.Sub(l.At)) + " ago"
		}
		_, _ = fmt.Fprintf(out, "%-12s %s\n", a.Name, who)
		_, _ = fmt.Fprintf(out, "%-12s %s · %s\n", "", a.ConfigDir(), left)
		for _, p := range account.Problems(a, auth) {
			_, _ = fmt.Fprintf(out, "%-12s ! %s\n", "", p)
		}
	}
	return 0
}

// nameRe is what an account may be called: one word, because it is typed on
// the command line and cycled through on a form.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func add(name, dir string, out, errOut io.Writer) int {
	if !nameRe.MatchString(name) {
		_, _ = fmt.Fprintf(errOut, "account: %q is not a name — letters, digits, - and _\n", name)
		return 2
	}
	if name == account.Default {
		_, _ = fmt.Fprintln(errOut, "account: \"default\" is your own login and is always there")
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "account: loading config:", err)
		return 1
	}
	if dir == "" {
		dir = "~/.claude-" + name
	}
	if prev, ok := cfg.Accounts[name]; ok && config.ExpandHome(prev) != config.ExpandHome(dir) {
		_, _ = fmt.Fprintf(errOut, "account: %s is already %s — remove it first to point it somewhere else\n", name, prev)
		return 1
	}
	a := account.Account{Name: name, Dir: dir}
	if a.ConfigDir() == account.DefaultConfigDir() {
		_, _ = fmt.Fprintln(errOut, "account: that directory is your own login — it is the default account already")
		return 1
	}
	if err := os.MkdirAll(a.ConfigDir(), 0o700); err != nil {
		_, _ = fmt.Fprintln(errOut, "account:", err)
		return 1
	}
	if cfg.Accounts == nil {
		cfg.Accounts = map[string]string{}
	}
	cfg.Accounts[name] = dir
	if err := config.Save(cfg); err != nil {
		_, _ = fmt.Fprintln(errOut, "account: saving config:", err)
		return 1
	}
	_, _ = fmt.Fprintf(out, "✓ %s → %s\n", name, a.ConfigDir())
	if err := initcmd.InstallFor(a); err != nil {
		_, _ = fmt.Fprintln(errOut, "account:", err)
		return 1
	}
	return login(name, out, errOut)
}

func login(name string, out, errOut io.Writer) int {
	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "account: loading config:", err)
		return 1
	}
	a, ok := account.Find(cfg, name)
	if !ok {
		_, _ = fmt.Fprintf(errOut, "account: no account %q — claude-dispatcher account add %s\n", name, name)
		return 1
	}
	if auth := account.Probe(a); !auth.LoggedIn {
		_, _ = fmt.Fprintf(out, "\nLogging %s in with Claude Code (CLAUDE_CONFIG_DIR=%s claude auth login)…\n", name, a.ConfigDir())
		if err := interactive(a, "auth", "login"); err != nil {
			_, _ = fmt.Fprintln(errOut, "account: login:", err)
			return 1
		}
	}
	auth := account.Probe(a)
	if auth.Known && !auth.LoggedIn {
		_, _ = fmt.Fprintln(errOut, "account: still not logged in — run claude-dispatcher account login", name)
		return 1
	}
	if !account.Onboarded(a) {
		_, _ = fmt.Fprintln(out, "\nClaude Code has not been set up in this directory yet, and a dispatch there would")
		_, _ = fmt.Fprintln(out, "stop on its first-run screens. Opening it now — finish them, then /exit.")
		_ = interactive(a)
	}
	who := auth.Email
	if auth.Plan != "" {
		who += " · " + auth.Plan
	}
	_, _ = fmt.Fprintf(out, "✓ %s is %s\n", name, strings.TrimPrefix(who, " · "))
	for _, p := range account.Problems(a, auth) {
		_, _ = fmt.Fprintln(out, "! "+p)
	}
	return 0
}

func remove(name string, out, errOut io.Writer) int {
	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "account: loading config:", err)
		return 1
	}
	dir, ok := cfg.Accounts[name]
	if !ok {
		_, _ = fmt.Fprintf(errOut, "account: no account %q\n", name)
		return 1
	}
	delete(cfg.Accounts, name)
	if err := config.Save(cfg); err != nil {
		_, _ = fmt.Fprintln(errOut, "account: saving config:", err)
		return 1
	}
	// The directory is the login and the transcripts of everything that ran
	// under it; resuming one of those still reads it, by the path on its
	// record. Deleting it is the human's call, not this command's.
	_, _ = fmt.Fprintf(out, "✓ forgot %s · %s is left as it was\n", name, dir)
	return 0
}

// interactive runs claude with args under a, on this terminal.
func interactive(a account.Account, args ...string) error {
	cmd := exec.Command("claude", args...)
	cmd.Env = account.CommandEnv(a)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "moments"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
