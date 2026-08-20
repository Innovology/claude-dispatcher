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
	// A scoped config makes no unscoped read of its own: the ambient key would
	// return the scoped products' issues all over again, under no product.
	for _, r := range got {
		if r.product == "" {
			t.Errorf("a scoped config must make no unscoped read, got %+v", r)
		}
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

func TestLinearReadsDedupesOneTokenNamedTwice(t *testing.T) {
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
	if got[0].product != "acme" || got[1].product != "cobalt" {
		t.Errorf("the first product in name order keeps the read: %+v", got)
	}
}
