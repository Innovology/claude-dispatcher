package dispatch

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Payment Retry Flow!":   "payment-retry-flow",
		"--already-sluggy--":    "already-sluggy",
		"  spaces  every/where": "spaces-every-where",
		"":                      "",
		"???":                   "",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShellQuoteRoundTrip(t *testing.T) {
	prompts := []string{
		"plain",
		"it's got 'quotes' in it",
		`double "quotes" and $vars and ` + "`backticks`",
		"multi\nline\nprompt",
	}
	for _, p := range prompts {
		out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(p)).Output()
		if err != nil {
			t.Fatalf("sh failed for %q: %v", p, err)
		}
		if string(out) != p {
			t.Errorf("round trip of %q gave %q", p, string(out))
		}
	}
}

func TestSlugifyStripsNonASCII(t *testing.T) {
	if got := Slugify("héllo wörld"); strings.Contains(got, "é") || strings.Contains(got, "ö") {
		t.Errorf("expected non-ascii stripped, got %q", got)
	}
}
