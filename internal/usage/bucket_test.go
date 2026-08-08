package usage

import "testing"

// Fable 5 is its own model family, not an Opus version — it used to fall into a
// nameless "other" row while accounting for ~40% of a week's tokens. Every Opus
// version, meanwhile, must land on the single "opus" row: the by-model split is
// by family, not by point release.
func TestBucketByFamilyNotVersion(t *testing.T) {
	for _, c := range []struct{ model, want string }{
		{"claude-fable-5", "fable"},
		{"claude-mythos-5", "mythos"},
		{"claude-mythos-preview", "mythos"},
		{"claude-opus-5", "opus"},
		{"claude-opus-4-8", "opus"},
		{"claude-opus-4-6", "opus"},
		{"claude-sonnet-5", "sonnet"},
		{"sonnet", "sonnet"},
		{"claude-haiku-4-5-20251001", "haiku"},
		// Unknown models keep their own id rather than vanishing into "other".
		{"claude-gizmo-6-20260401", "gizmo-6"},
		{"<synthetic>", "<synthetic>"},
		{"", "unknown"},
	} {
		if got := bucket(c.model); got != c.want {
			t.Errorf("bucket(%q) = %q, want %q", c.model, got, c.want)
		}
	}
}
