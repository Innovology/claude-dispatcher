// Package transcript extracts a best-effort activity preview from a Claude
// Code session transcript. The JSONL format is documented as internal and
// version-unstable, so everything here is defensive and failures degrade to
// an empty preview — hooks, not transcripts, are the source of truth for
// status.
package transcript

import (
	"encoding/json"
	"io"
	"os"
	"strings"
)

const tailBytes = 128 * 1024

type line struct {
	Type    string `json:"type"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Name string `json:"name"` // tool_use
}

// Tail returns up to n recent activity lines (assistant text and tool uses),
// oldest first.
func Tail(path string, n int) []string {
	if path == "" || n <= 0 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	if fi, err := f.Stat(); err == nil && fi.Size() > tailBytes {
		_, _ = f.Seek(fi.Size()-tailBytes, io.SeekStart)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}

	var out []string
	rows := strings.Split(string(data), "\n")
	for i := len(rows) - 1; i >= 0 && len(out) < n; i-- {
		row := strings.TrimSpace(rows[i])
		if row == "" || !strings.HasPrefix(row, "{") {
			continue
		}
		var l line
		if json.Unmarshal([]byte(row), &l) != nil || l.Type != "assistant" {
			continue
		}
		// Blocks are appended reversed so the final whole-slice flip restores
		// their in-message order.
		blocks := renderContent(l.Message.Content)
		for i := len(blocks) - 1; i >= 0; i-- {
			if len(out) < n {
				out = append(out, blocks[i])
			}
		}
	}
	// Collected newest-first; flip for display.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Usage is what the newest assistant turn had in its context window, plus the
// model that ran it.
//
// Tokens deliberately INCLUDES cache reads, which internal/usage deliberately
// excludes: usage answers "what did this cost against the subscription", where
// cache reads count for almost nothing, and this answers "how full is the
// context window", where a cached token occupies a slot like any other.
//
// There is no output_tokens term for the same reason: the assistant's reply is
// not yet in the window it was generated from.
type Usage struct {
	Model  string
	Tokens int
}

type usageLine struct {
	Type    string `json:"type"`
	Message struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// LastUsage reports the newest assistant turn's context occupancy. ok is false
// when the file is unreadable or carries no assistant turn with usage — an
// answer of zero tokens would be a claim about the session, not an observation.
func LastUsage(path string) (Usage, bool) {
	if path == "" {
		return Usage{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return Usage{}, false
	}
	defer func() { _ = f.Close() }()
	// Same bounded tail as Tail: a long session's transcript is megabytes, and
	// the newest turn is always at the end of it.
	if fi, err := f.Stat(); err == nil && fi.Size() > tailBytes {
		_, _ = f.Seek(fi.Size()-tailBytes, io.SeekStart)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return Usage{}, false
	}
	rows := strings.Split(string(data), "\n")
	for i := len(rows) - 1; i >= 0; i-- {
		row := strings.TrimSpace(rows[i])
		if row == "" || !strings.HasPrefix(row, "{") {
			continue
		}
		var l usageLine
		if json.Unmarshal([]byte(row), &l) != nil || l.Type != "assistant" {
			continue
		}
		u := l.Message.Usage
		tok := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
		if tok <= 0 {
			continue
		}
		return Usage{Model: l.Message.Model, Tokens: tok}, true
	}
	return Usage{}, false
}

func renderContent(raw json.RawMessage) []string {
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		var s string
		if json.Unmarshal(raw, &s) == nil && strings.TrimSpace(s) != "" {
			return []string{firstLine(s)}
		}
		return nil
	}
	var out []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" {
				out = append(out, firstLine(t))
			}
		case "tool_use":
			if b.Name != "" {
				out = append(out, "⚙ "+b.Name)
			}
		}
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
