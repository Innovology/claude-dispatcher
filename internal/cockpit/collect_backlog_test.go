package cockpit

// collect_backlog_test.go covers linearReads — which Linear calls a load makes.
// It is the whole of the scoping rule: a token sees one workspace and only the
// teams Linear granted it, so which token reads a product is what scopes it.

import (
	"strings"
	"time"

	"claude-dispatcher/internal/linear"
	"claude-dispatcher/internal/state"
	"testing"

	"claude-dispatcher/internal/config"
)

func TestLinearReadsUnscoped(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "")
	if got := linearReads(nil); got != nil {
		t.Errorf("no config and no key must read nothing, got %+v", got)
	}
	if got := linearReads(&config.Config{}); got != nil {
		t.Errorf("a config naming no token, with no key, must read nothing, got %+v", got)
	}

	// The install that predates scoping: one key, one read, no product.
	t.Setenv("LINEAR_API_KEY", "lin_api_ambient")
	got := linearReads(&config.Config{})
	if len(got) != 1 {
		t.Fatalf("expected one unscoped read, got %+v", got)
	}
	if got[0].key != "lin_api_ambient" || got[0].product != "" {
		t.Errorf("unscoped read = %+v", got[0])
	}
}

// TestLinearReadsPerProduct is the shape this exists for: several products, a
// token each — including two products in one workspace, told apart by holding a
// team-scoped key apiece rather than by anything this code does.
func TestLinearReadsPerProduct(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "")
	cfg := &config.Config{Linear: map[string]string{
		"acme":    "lin_api_acme",
		"bluefin": "lin_api_blu",
		"cobalt":  "lin_api_cob",
	}}
	got := linearReads(cfg)
	if len(got) != 3 {
		t.Fatalf("expected a read per product, got %+v", got)
	}
	// Name order, so a load cannot reshuffle them.
	if got[0].product != "acme" || got[1].product != "bluefin" || got[2].product != "cobalt" {
		t.Fatalf("reads out of name order: %+v", got)
	}
	if got[0].key != "lin_api_acme" || got[1].key != "lin_api_blu" || got[2].key != "lin_api_cob" {
		t.Errorf("each product must be read with its own token: %+v", got)
	}
	// With no ambient key there is nothing else to read: a product per token and
	// no unscoped read tacked on the end.
	for _, r := range got {
		if r.product == "" {
			t.Errorf("no ambient key means no unscoped read, got %+v", r)
		}
	}
}

// TestLinearReadsKeepsTheUnscopedRead is the non-destructive rule: filling in
// one line of [linear] must not empty the backlog that was already reading. The
// ambient key is another workspace as easily as the same one, so it stays a read
// of its own — and goes last, so a scoped read keeps the product tag on any
// issue both of them return.
func TestLinearReadsKeepsTheUnscopedRead(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "lin_api_ambient")
	got := linearReads(&config.Config{Linear: map[string]string{"acme": "lin_api_acme"}})
	if len(got) != 2 {
		t.Fatalf("expected the scoped read and the ambient one, got %+v", got)
	}
	if got[0].product != "acme" || got[0].key != "lin_api_acme" {
		t.Errorf("scoped read = %+v", got[0])
	}
	if got[1].product != "" || got[1].key != "lin_api_ambient" {
		t.Errorf("the ambient read must come last and name no product: %+v", got[1])
	}

	// Unless a product has already claimed it, in which case it is that
	// product's read and asking again would be the same call twice.
	got = linearReads(&config.Config{Linear: map[string]string{"acme": "lin_api_ambient"}})
	if len(got) != 1 || got[0].product != "acme" {
		t.Errorf("a product naming the ambient key is one read, got %+v", got)
	}
}

func TestLinearReadsFallsBackToUnscopedKey(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "lin_api_ambient")
	cfg := &config.Config{Linear: map[string]string{"acme": ""}}
	got := linearReads(cfg)
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got[0].key != "lin_api_ambient" || got[0].product != "acme" {
		t.Errorf("read = %+v, want the unscoped key scoped to acme", got[0])
	}
}

func TestLinearReadsSkipsProductWithNoToken(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "")
	cfg := &config.Config{Linear: map[string]string{
		"acme":    "", // no token of its own, none ambient
		"bluefin": "lin_api_bluefin",
	}}
	got := linearReads(cfg)
	if len(got) != 1 || got[0].product != "bluefin" {
		t.Fatalf("a product with no token to ask with must be skipped, got %+v", got)
	}
}

// TestLinearReadsSharedTokenNamesNoProduct: one token named twice is one read,
// and that read names neither claimant. A shared token cannot say which of them
// an issue belongs to, and filing every sharer's tickets under whichever
// product sorts first would be stable and wrong.
func TestLinearReadsSharedTokenNamesNoProduct(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "")
	cfg := &config.Config{Linear: map[string]string{
		"acme":    "lin_api_shared",
		"bluefin": "lin_api_shared",
		"cobalt":  "lin_api_cobalt",
	}}
	got := linearReads(cfg)
	if len(got) != 2 {
		t.Fatalf("one token named twice is one read, got %+v", got)
	}
	if got[0].key != "lin_api_shared" || got[0].product != "" {
		t.Errorf("a shared token must name no product: %+v", got[0])
	}
	// Name order still decides the order the reads merge in — it just does not
	// decide who gets credited for a token two products hold.
	if got[1].product != "cobalt" || got[1].key != "lin_api_cobalt" {
		t.Errorf("a token one product holds still names it: %+v", got[1])
	}
}

// TestLinearReadsSharedFallbackNamesNoProduct is the same rule reached the other
// way: two products naming no token of their own both fall back to the ambient
// key, which then speaks for neither.
func TestLinearReadsSharedFallbackNamesNoProduct(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "lin_api_ambient")
	cfg := &config.Config{Linear: map[string]string{"acme": "", "bluefin": ""}}
	got := linearReads(cfg)
	if len(got) != 1 {
		t.Fatalf("one fallback token is one read, got %+v", got)
	}
	if got[0].key != "lin_api_ambient" || got[0].product != "" {
		t.Errorf("read = %+v, want the ambient key under no product", got[0])
	}
}

// ---- the merge --------------------------------------------------------------
//
// linearReads decides which calls to go and make; linearTickets is what happens
// to the answers, and it holds the two rules this source is built on — dedupe by
// issue id, and the product tag comes from the read. Both were behind an HTTP
// call until linearTickets was split out, so neither had a test.

func linIssue(id, identifier, title string) linear.Issue {
	return linear.Issue{
		ID: id, Identifier: identifier, Title: title,
		Priority: "Medium", State: "Todo",
		UpdatedAt: time.Now().Add(-2 * time.Hour),
	}
}

// Every ticket carries the product of the read it came back on, and a read that
// names no product (the unscoped one, or a token two products share) leaves it
// blank rather than borrowing its neighbour's.
func TestLinearTicketsCarryTheirRead(t *testing.T) {
	reads := []linearRead{{product: "acme", key: "k1"}, {key: "k2"}}
	got := linearTickets(reads, [][]linear.Issue{
		{linIssue("uuid-1", "ENG-1", "scoped")},
		{linIssue("uuid-2", "OPS-9", "unscoped")},
	}, nil)

	if len(got) != 2 {
		t.Fatalf("got %d tickets, want 2", len(got))
	}
	if got[0].product != "acme" || got[0].id != "ENG-1" || got[0].src != "lin" {
		t.Errorf("scoped ticket = %+v", got[0])
	}
	if got[1].product != "" {
		t.Errorf("a read naming no product must not tag its tickets: %+v", got[1])
	}
	// The prompt is the ticket's own words, and the id is the identifier — which
	// is what the picked set and the branch slug are keyed by.
	if !strings.Contains(got[0].prompt, "ENG-1") || !strings.Contains(got[0].prompt, "scoped") {
		t.Errorf("prompt = %q", got[0].prompt)
	}
}

// Two team-scoped tokens can overlap on a team, so the same issue comes back
// twice. It must appear once, and keep the product of the read that named it
// first — reads are in product-name order, so that answer is stable.
func TestLinearTicketsDedupeOnIssueID(t *testing.T) {
	shared := linIssue("uuid-shared", "ENG-7", "one issue, two tokens")
	reads := []linearRead{{product: "acme", key: "k1"}, {product: "bluefin", key: "k2"}}
	got := linearTickets(reads, [][]linear.Issue{
		{shared, linIssue("uuid-a", "ENG-8", "acme only")},
		{shared, linIssue("uuid-b", "ENG-9", "bluefin only")},
	}, nil)

	if len(got) != 3 {
		t.Fatalf("got %d tickets, want 3 — the shared issue must appear once: %+v", len(got), got)
	}
	if got[0].id != "ENG-7" || got[0].product != "acme" {
		t.Errorf("the first read to name it keeps it: %+v", got[0])
	}
	for _, tk := range got[1:] {
		if tk.id == "ENG-7" {
			t.Errorf("ENG-7 appears twice: %+v", got)
		}
	}
}

// Deduped on the id and NOT the identifier: two workspaces both keying a team
// ENG can each raise an ENG-124, and they are different tickets. Dropping the
// second would lose one nobody ever saw, which is the one failure a backlog
// must not have — even though keeping it means one `space` ticks both rows.
func TestLinearTicketsKeepTwoWorkspacesSharingAnIdentifier(t *testing.T) {
	reads := []linearRead{{product: "acme", key: "k1"}, {product: "bluefin", key: "k2"}}
	got := linearTickets(reads, [][]linear.Issue{
		{linIssue("uuid-acme", "ENG-124", "acme's ENG-124")},
		{linIssue("uuid-blue", "ENG-124", "bluefin's ENG-124")},
	}, nil)

	if len(got) != 2 {
		t.Fatalf("two workspaces' ENG-124s are two tickets, got %d: %+v", len(got), got)
	}
	if got[0].title == got[1].title {
		t.Errorf("the wrong one was dropped: %+v", got)
	}
	if got[0].product != "acme" || got[1].product != "bluefin" {
		t.Errorf("each keeps its own read's product: %+v", got)
	}
}

// A read that failed left a nil row in readIssues — the collector drops the
// error and keeps the slot — and contributes nothing rather than panicking.
func TestLinearTicketsToleratesAFailedRead(t *testing.T) {
	reads := []linearRead{{product: "acme", key: "k1"}, {product: "bluefin", key: "k2"}}
	got := linearTickets(reads, [][]linear.Issue{
		nil,
		{linIssue("uuid-b", "ENG-9", "bluefin only")},
	}, nil)
	if len(got) != 1 || got[0].product != "bluefin" {
		t.Fatalf("a failed read contributes nothing: %+v", got)
	}

	// And nothing at all is an empty backlog, not a crash.
	if got := linearTickets(nil, nil, nil); len(got) != 0 {
		t.Errorf("no reads = no tickets, got %+v", got)
	}
	// A short readIssues (never happens in the collector, but the two are
	// separate arguments now) must not index past the end.
	if got := linearTickets(reads, nil, nil); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

// An in-flight dispatch on a ticket's own branch marks the row taken, so the
// backlog does not offer work that is already out. That join is by identifier.
func TestLinearTicketsReportWhatIsAlreadyDispatched(t *testing.T) {
	records := []*state.Dispatch{{
		Feature: "eng-1", Branch: "feature/eng-1", Status: state.StatusWorking,
	}}
	got := linearTickets(
		[]linearRead{{product: "acme", key: "k1"}},
		[][]linear.Issue{{linIssue("uuid-1", "ENG-1", "already out")}},
		records,
	)
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got[0].taken != "eng-1" {
		t.Errorf("taken = %q, want the dispatch working it", got[0].taken)
	}
}
