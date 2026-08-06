package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/transcript"
)

// Breakpoints: below wideAt a single pane; below ultraAt two panes; at or
// above ultraAt three. Wide screens get more panes, never one ballooned view.
const (
	wideAt  = 110
	ultraAt = 170
)

var (
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	headerStyle = lipgloss.NewStyle().Bold(true)
	noticeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	selStyle    = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("236"))
	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1)

	statusStyles = map[state.Status]lipgloss.Style{
		state.StatusLaunching:  lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		state.StatusWorking:    lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		state.StatusNeedsInput: lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		state.StatusBlocked:    lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		state.StatusDone:       lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		state.StatusExited:     dimStyle,
	}
)

func statusGlyph(s state.Status) string {
	switch s {
	case state.StatusLaunching:
		return "…"
	case state.StatusWorking:
		return "●"
	case state.StatusNeedsInput:
		return "◆"
	case state.StatusBlocked:
		return "■"
	case state.StatusDone:
		return "✓"
	case state.StatusExited:
		return "✗"
	}
	return "?"
}

func statusLabel(s state.Status) string {
	switch s {
	case state.StatusNeedsInput:
		return "needs you"
	default:
		return string(s)
	}
}

func (m model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	header := m.headerView()
	footer := m.footerView()
	bodyH := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	var body string
	if m.mode == modeForm && m.form != nil {
		body = pane("dispatch", m.formView(), m.width, bodyH)
	} else {
		body = m.tiledView(m.width, bodyH)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m model) tiledView(w, h int) string {
	switch {
	case w < wideAt:
		return pane("dispatchers", m.tableView(w-4), w, h)
	case w < ultraAt:
		lw := w * 58 / 100
		return lipgloss.JoinHorizontal(lipgloss.Top,
			pane("dispatchers", m.tableView(lw-4), lw, h),
			pane("detail", m.detailView(w-lw-4, h-4), w-lw, h),
		)
	default:
		lw := w * 42 / 100
		dw := w * 33 / 100
		sw := w - lw - dw
		return lipgloss.JoinHorizontal(lipgloss.Top,
			pane("dispatchers", m.tableView(lw-4), lw, h),
			pane("detail", m.detailView(dw-4, h-4), dw, h),
			pane("shipping — today", m.shipView(sw-4), sw, h),
		)
	}
}

func pane(title, content string, w, h int) string {
	inner := titleStyle.Render(title) + "\n" + content
	return borderStyle.Width(w - 2).Height(h - 2).Render(inner)
}

func (m model) headerView() string {
	left := headerStyle.Render(" ⚡ claude dispatch ")
	counts := map[state.Status]int{}
	for _, d := range m.dispatches {
		counts[d.Status]++
	}
	var parts []string
	for _, s := range []state.Status{
		state.StatusBlocked, state.StatusNeedsInput, state.StatusWorking,
		state.StatusLaunching, state.StatusDone, state.StatusExited,
	} {
		if counts[s] > 0 {
			parts = append(parts, statusStyles[s].Render(
				fmt.Sprintf("%s %d %s", statusGlyph(s), counts[s], statusLabel(s))))
		}
	}
	right := strings.Join(parts, dimStyle.Render("  ·  ")) + " "
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m model) footerView() string {
	var help string
	if m.mode == modeForm && m.form != nil {
		switch m.form.step {
		case stepRepo:
			help = "type to filter · ↑/↓ · enter select repo · esc cancel"
		case stepFeature:
			help = "enter continue · esc back"
		case stepPrompt:
			help = "ctrl+d dispatch · esc back"
		}
		if m.form.errMsg != "" {
			return " " + errStyle.Render(m.form.errMsg) + dimStyle.Render("  ·  "+help)
		}
	} else {
		help = "n dispatch · enter attach · d shipped · x kill · r refresh · q quit"
	}
	if m.notice != "" {
		return " " + noticeStyle.Render(m.notice) + dimStyle.Render("  ·  "+help)
	}
	return " " + dimStyle.Render(help)
}

func (m model) tableView(w int) string {
	if len(m.dispatches) == 0 {
		return dimStyle.Render("\n  no dispatchers out — press n to dispatch one")
	}
	stateW, ageW := 12, 5
	rest := w - stateW - ageW - 6
	featW := max(10, rest*55/100)
	repoW := max(8, rest-featW)

	head := dimStyle.Render(fmt.Sprintf("  %-*s %-*s %-*s %*s",
		featW, "FEATURE", repoW, "REPO", stateW, "STATE", ageW, "AGE"))
	rows := []string{head}
	for i, d := range m.dispatches {
		marker := "  "
		if i == m.cursor {
			marker = "▸ "
		}
		row := fmt.Sprintf("%s%-*s %-*s %-*s %*s",
			marker,
			featW, truncate(d.Feature, featW),
			repoW, truncate(d.RepoName, repoW),
			stateW, truncate(statusGlyph(d.Status)+" "+statusLabel(d.Status), stateW),
			ageW, humanAge(time.Since(d.CreatedAt)),
		)
		styled := statusStyles[d.Status].Render(row)
		if i == m.cursor {
			styled = selStyle.Render(statusStyles[d.Status].Inline(true).Render(row))
		}
		rows = append(rows, styled)
	}
	return strings.Join(rows, "\n")
}

func (m model) detailView(w, h int) string {
	d := m.selected()
	if d == nil {
		return dimStyle.Render("nothing selected")
	}
	sid := d.SessionID
	if len(sid) > 8 {
		sid = sid[:8]
	}
	lines := []string{
		headerStyle.Render(truncate(d.Feature, w)),
		dimStyle.Render(truncate(d.RepoPath, w)),
		"",
		kv("status", statusStyles[d.Status].Render(statusGlyph(d.Status)+" "+statusLabel(d.Status)), w),
		kv("why", d.StatusReason, w),
		kv("branch", d.Branch, w),
		kv("product", orDash(d.Product), w),
		kv("tmux", d.TmuxSession, w),
		kv("session", orDash(sid), w),
		kv("pr", prLabel(d), w),
		kv("commits", fmt.Sprintf("%d", len(d.Commits)), w),
		kv("updated", humanAge(time.Since(d.UpdatedAt))+" ago", w),
		"",
		dimStyle.Render("prompt"),
		truncate(strings.ReplaceAll(d.Prompt, "\n", " "), w*2),
	}
	if tail := transcript.Tail(d.TranscriptPath, 5); len(tail) > 0 {
		lines = append(lines, "", dimStyle.Render("recent activity"))
		for _, t := range tail {
			lines = append(lines, truncate(t, w))
		}
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

func (m model) shipView(w int) string {
	s := m.ship
	if s.CollectedAt.IsZero() {
		return dimStyle.Render("collecting…")
	}
	prs := "—"
	if s.PRsOK {
		prs = fmt.Sprintf("%d", s.PRsToday)
	}
	lines := []string{
		kv("commits", fmt.Sprintf("%d", s.Commits), w),
		kv("via dispatch", fmt.Sprintf("%d (%d%%)", s.Dispatched, s.DispatchedPct()), w),
		kv("prs launched", prs, w),
		kv("features live", fmt.Sprintf("%d", s.FeaturesLive), w),
		kv("repos active", fmt.Sprintf("%d of %d", s.ReposActive, s.ReposTotal), w),
		"",
		dimStyle.Render(fmt.Sprintf("as of %s · all branches", s.CollectedAt.Format("15:04"))),
	}
	return strings.Join(lines, "\n")
}

func (m model) formView() string {
	f := m.form
	stepTitle := func(n int, label string, active bool) string {
		s := fmt.Sprintf("%d. %s", n, label)
		if active {
			return headerStyle.Render(s)
		}
		return dimStyle.Render(s)
	}
	var b strings.Builder
	b.WriteString(stepTitle(1, "repo", f.step == stepRepo))
	if f.step > stepRepo {
		b.WriteString(dimStyle.Render("  →  ") + f.repo.Name)
	}
	b.WriteString("\n")
	switch f.step {
	case stepRepo:
		b.WriteString(dimStyle.Render("filter: ") + f.filter + "▏\n\n")
		visible := f.filtered()
		shown := min(len(visible), 15)
		for i := 0; i < shown; i++ {
			marker := "  "
			line := visible[i].Name
			if visible[i].Product != "" {
				line += dimStyle.Render("  (" + visible[i].Product + ")")
			}
			if i == f.cursor {
				marker = "▸ "
				line = headerStyle.Render(visible[i].Name)
				if visible[i].Product != "" {
					line += dimStyle.Render("  (" + visible[i].Product + ")")
				}
			}
			b.WriteString(marker + line + "\n")
		}
		if len(visible) > shown {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  … %d more, keep typing\n", len(visible)-shown)))
		}
	case stepFeature:
		b.WriteString("\n" + stepTitle(2, "feature name", true) + "\n")
		b.WriteString(f.feature.View() + "\n")
		b.WriteString(dimStyle.Render("names the branch (feature/<slug>) and the unit of history"))
	case stepPrompt:
		b.WriteString(stepTitle(2, "feature", false) + dimStyle.Render("  →  ") + f.featureName() + "\n\n")
		b.WriteString(stepTitle(3, "prompt", true) + "\n")
		b.WriteString(f.prompt.View())
	}
	return b.String()
}

func kv(k, v string, w int) string {
	return dimStyle.Render(fmt.Sprintf("%-15s", k)) + truncate(v, max(1, w-15))
}

func prLabel(d *state.Dispatch) string {
	if d.PRNumber == 0 {
		return "—"
	}
	label := fmt.Sprintf("#%d %s", d.PRNumber, strings.ToLower(d.PRState))
	if d.DeployedAt != nil {
		label += " · live " + humanAge(time.Since(*d.DeployedAt)) + " ago"
	}
	return label
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
