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
