package account

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"

	"claude-dispatcher/internal/state"
)

// StatusLineMarker is what identifies our status line in a settings.json, the
// way the hook marker identifies our hooks.
const StatusLineMarker = "claude-dispatcher statusline"

// RunStatusLine is `claude-dispatcher statusline [--then <command>]`, the
// status line command installed in every account's settings.
//
// It keeps the payload's rate limits as the reading for the config directory
// the session runs under — CLAUDE_CONFIG_DIR as the session has it, which is
// how one installed command files every account's figures under the right
// one — and then draws whatever the human's own status line drew: --then is
// the command that was there before, run on the same input. With none it
// draws nothing, so installing it changes nothing anyone sees.
//
// Like the hook it must never disturb the session, so it always exits 0 and a
// reading it cannot keep is dropped without a word.
func RunStatusLine(args []string) int {
	raw, _ := io.ReadAll(os.Stdin)
	_ = RecordStatusLine(state.Dir(), DefaultConfigDir(), raw, time.Now())
	if len(args) >= 2 && args[0] == "--then" && args[1] != "" {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", args[1])
		} else {
			cmd = exec.Command("sh", "-c", args[1])
		}
		cmd.Stdin = bytes.NewReader(raw)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}
	return 0
}
