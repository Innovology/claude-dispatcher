package cockpit

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// dispWidth is the rendered column width of s, ignoring any ANSI colour codes.
func dispWidth(s string) int { return lipgloss.Width(s) }

// truncate clips s to at most w columns, adding an ellipsis when it overflows.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if dispWidth(s) <= w {
		return s
	}
	r := []rune(s)
	if w == 1 {
		return "…"
	}
	// Trim rune-by-rune until the visible width fits with room for the ellipsis.
	for len(r) > 0 && dispWidth(string(r)) > w-1 {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// padTo pads s with spaces to exactly w columns. align: alignLeft or alignRight.
func padTo(s string, w, align int) string {
	d := dispWidth(s)
	if d >= w {
		return s
	}
	pad := strings.Repeat(" ", w-d)
	if align == alignRight {
		return pad + s
	}
	return s + pad
}

const (
	alignLeft  = 0
	alignRight = 1
)

// seg is one cell in a row. A seg with flex=true absorbs the leftover width
// after all fixed segs are placed (flex width splits evenly across flex segs).
// hex is the foreground colour; bg is the (usually shared) row background.
type seg struct {
	text  string
	w     int    // fixed width in columns (ignored when flex)
	flex  bool   // grow to fill remaining width
	align int    // alignLeft | alignRight
	hex   string // foreground colour, "" for default
	bg    string // background colour, "" for none
}

// c builds a fixed-width left-aligned cell.
func c(text string, w int, hex string) seg { return seg{text: text, w: w, hex: hex} }

// cr builds a fixed-width right-aligned cell.
func cr(text string, w int, hex string) seg {
	return seg{text: text, w: w, align: alignRight, hex: hex}
}

// flex builds a cell that grows to fill the remaining row width.
func flexc(text string, hex string) seg { return seg{text: text, flex: true, hex: hex} }

// row composes segs into a single line exactly total columns wide. A shared
// background bg (e.g. the selection highlight) is painted under every cell,
// including the inter-cell padding, so the bar is unbroken.
func row(total int, bg string, segs ...seg) string {
	fixed := 0
	flexCount := 0
	for _, s := range segs {
		if s.flex {
			flexCount++
		} else {
			fixed += s.w
		}
	}
	flexW := 0
	if flexCount > 0 {
		flexW = (total - fixed) / flexCount
		if flexW < 0 {
			flexW = 0
		}
	}
	var b strings.Builder
	used := 0
	for i, s := range segs {
		w := s.w
		if s.flex {
			w = flexW
			// Last flex cell soaks up any rounding remainder.
			if i == len(segs)-1 {
				w = total - used
			}
		}
		if w <= 0 {
			continue
		}
		txt := padTo(truncate(s.text, w), w, s.align)
		b.WriteString(paint(s.hex, orBg(s.bg, bg), txt))
		used += w
	}
	if used < total {
		b.WriteString(paint("", bg, strings.Repeat(" ", total-used)))
	}
	return b.String()
}

func orBg(cell, rowBg string) string {
	if cell != "" {
		return cell
	}
	return rowBg
}

// line colours a whole string on a background and pads it to width w. Used for
// free-form (non-columnar) lines that still need a selection background.
func line(s string, w int, hex, bg string) string {
	txt := padTo(truncate(s, w), w, alignLeft)
	return paint(hex, bg, txt)
}

// bar renders a proportional meter pct% full over `width` cells (█ filled, ░
// empty). Mirrors the design's bar().
func bar(pct, width int) string {
	n := (pct * width) / 100
	if n < 0 {
		n = 0
	}
	if n > width {
		n = width
	}
	return strings.Repeat("█", n) + strings.Repeat("░", width-n)
}

// blank returns w spaces (an empty cell in a fixed grid).
func blank(w int) string { return strings.Repeat(" ", w) }

// hpad left-pads content with n leading spaces on every line (a left gutter,
// like the design's padding:0 24px).
func gutter(content string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

// vjoin stacks lines top to bottom.
func vjoin(lines ...string) string { return strings.Join(lines, "\n") }

// clampLines trims content to at most h lines so a pane never overflows its box.
func clampLines(content string, h int) string {
	if h <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// padBlockTo grows content to exactly h lines by appending blank lines, so
// side-by-side panes align to the same height.
func padBlockTo(content string, h int) string {
	lines := strings.Split(content, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// vrule is a full-height vertical divider column of the given colour.
func vrule(h int, hex string) string {
	lines := make([]string, h)
	for i := range lines {
		lines[i] = fg(hex, "│")
	}
	return strings.Join(lines, "\n")
}

// hjoin places blocks side by side, top-aligned. Thin wrapper over lipgloss so
// lenses do not each import it.
func hjoin(blocks ...string) string {
	return lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
}
