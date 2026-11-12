package gh

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The backlog sorts by when an issue was filed as well as when it last moved,
// so the one search that fills it has to ask for both — and file them under the
// repo's bare name, which is how a discovered repo is named.
func TestAssignedIssuesCarriesBothDates(t *testing.T) {
	calls := fakeGH(t, `case "$*" in
  *"search issues"*) cat <<'EOF'
[{"repository":{"name":"shop-api","nameWithOwner":"acme/shop-api"},"number":12,
  "title":"Checkout times out","url":"u","body":"b","state":"open",
  "labels":[{"name":"bug"}],
  "createdAt":"2026-07-01T08:00:00Z","updatedAt":"2026-08-19T10:00:00Z"}]
EOF
  ;;
  *) echo '[]' ;;
esac`)

	byRepo, ok, capped := AssignedIssues()
	if !ok || capped {
		t.Fatalf("the search answered one issue: ok %v, capped %v", ok, capped)
	}
	got := byRepo["shop-api"]
	if len(got) != 1 {
		t.Fatalf("issues = %+v", byRepo)
	}
	is := got[0]
	if is.Number != 12 || is.Title != "Checkout times out" || !slices.Equal(is.Labels, []string{"bug"}) {
		t.Errorf("issue = %+v", is)
	}
	if !is.CreatedAt.Equal(time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("CreatedAt = %v", is.CreatedAt)
	}
	if !is.UpdatedAt.Equal(time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("UpdatedAt = %v", is.UpdatedAt)
	}
	if c := calls(); len(c) != 1 || !strings.Contains(c[0], "createdAt") || !strings.Contains(c[0], "--limit 1000") {
		t.Errorf("one search asking for createdAt, up to the search's own limit, got %q", c)
	}
}

// A search that comes back full may have stopped short, and says so.
func TestAssignedIssuesSaysWhenItStoppedAtTheLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < AssignedLimit; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"repository":{"name":"shop-api"},"number":` + strconv.Itoa(i+1) + `,"title":"t"}`)
	}
	b.WriteString("]")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "issues.json"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeGH(t, `case "$*" in
  *"search issues"*) cat `+filepath.Join(dir, "issues.json")+` ;;
  *) echo '[]' ;;
esac`)
	byRepo, ok, capped := AssignedIssues()
	if !ok || !capped || len(byRepo["shop-api"]) != AssignedLimit {
		t.Errorf("ok %v, capped %v, %d issues", ok, capped, len(byRepo["shop-api"]))
	}
}
