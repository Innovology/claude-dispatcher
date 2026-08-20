package cockpit

// collect_backlog_test.go covers linearReads — which Linear calls a load makes.
// It is the whole of the scoping rule: a token sees one workspace and only the
// teams Linear granted it, so which token reads a product is what scopes it.

import (
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
