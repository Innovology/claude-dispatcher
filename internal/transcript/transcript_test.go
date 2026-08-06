package transcript

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const fixture = `{"type":"user","message":{"role":"user","content":"ignore me"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"first answer\nwith a second line"}]}}
this line is garbage, not json
{"type":"assistant","message":{"role":"assistant","content":"plain string content"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"},{"type":"text","text":"done"}]}}
`

func writeFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTailOrderAndContent(t *testing.T) {
	got := Tail(writeFixture(t), 10)
	want := []string{"first answer", "plain string content", "⚙ Bash", "done"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tail = %v, want %v", got, want)
	}
}

func TestTailLimitKeepsNewest(t *testing.T) {
	got := Tail(writeFixture(t), 2)
	want := []string{"⚙ Bash", "done"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tail(2) = %v, want %v", got, want)
	}
}

func TestTailDegradesToNil(t *testing.T) {
	if Tail("", 5) != nil {
		t.Error("empty path should be nil")
	}
	if Tail("/nonexistent/file.jsonl", 5) != nil {
		t.Error("missing file should be nil")
	}
	if Tail(writeFixture(t), 0) != nil {
		t.Error("n=0 should be nil")
	}
}
