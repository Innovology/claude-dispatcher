package cockpit

import (
	"strconv"

	"github.com/charmbracelet/x/ansi"
)

// itoa is strconv.Itoa under a short name — this file renders a lot of counts.
func itoa(n int) string { return strconv.Itoa(n) }

// truncateAnsi clips an already-colour-rendered string to w display columns
// without cutting an escape sequence in half.
func truncateAnsi(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// pct returns 100*a/b, guarding against divide-by-zero.
func pct(a, b int) int {
	if b == 0 {
		return 0
	}
	return 100 * a / b
}

// min/max on ints (Go's builtins exist in 1.21+, but keep explicit for clarity
// where readability matters).
func mini(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// clampCursor keeps an index within [0, n-1], returning 0 when the list empty.
func clampCursor(i, n int) int {
	if n <= 0 {
		return 0
	}
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}
