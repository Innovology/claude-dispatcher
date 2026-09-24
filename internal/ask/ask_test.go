package ask

import "testing"

// The fixtures are real turn endings from the transcripts ADR 0017 measured,
// each followed in the log by the human attaching to type a one-line answer.
func TestOf(t *testing.T) {
	cases := []struct{ name, said, want string }{
		{"question closes the message",
			"PR #70 is still open, all checks green, and `mergeStateStatus` is `BLOCKED`.\n\nWant me to merge it now?",
			"Want me to merge it now?"},
		{"invitation without a question mark",
			"I've left the merge to you — main auto-deploys, and that's your call rather than mine. Say the word and I'll merge it.",
			"Say the word and I'll merge it."},
		{"ask above a trailing caveat",
			"Want me to wire both in? It's small enough to be one PR.",
			"Want me to wire both in?"},
		{"emphasis stripped, list item split from its lead",
			"Two options:\n\n**Say the word.**\n- Env-var cutover — set `ATLAS_*` on both apps.",
			"Say the word."},
		{"a report asks nothing",
			"PR #700 is open. Watching CI.\n\nProduction right now: core on `a7761e3e`, serving 200.",
			""},
		{"a question far above the close is one the message went past",
			"Should we split it?\n\nPara two.\n\nPara three.\n\nPara four. Done.",
			""},
		{"empty", "", ""},
	}
	for _, c := range cases {
		if got := Of(c.said); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
