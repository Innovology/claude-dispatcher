package cockpit

// dispatchform.go is the in-cockpit "new dispatch" overlay: the classic
// cockpit's repo → feature → prompt flow, ported into v2 so ad-hoc work can be
// dispatched without a backlog ticket. Open it with `+` or the palette's
// "dispatch" / "new dispatch" command. Like settings, it lives behind a pointer
// on the model so its textinputs keep focus state across value-receiver Update
// copies. Submitting hands off to launchCmd, which does the real dispatch.

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/repos"
)

type dispatchStep int

const (
	dispatchRepo dispatchStep = iota
	dispatchFeature
	dispatchPrompt
)

// dispatchForm is the open new-dispatch overlay. Each step owns one textinput;
// keeping three (rather than reusing one) preserves what you typed when you esc
// back a step.
type dispatchForm struct {
	step   dispatchStep
	repos  []repos.Repo
	cursor int
	repo   repos.Repo

	filter  textinput.Model // step 1: filter repos by name/product
	feature textinput.Model // step 2: feature name
	prompt  textinput.Model // step 3: the prompt

	errMsg string
}

// newDispatchForm builds the overlay, discovering repos from cfg. It focuses
// the filter input; the caller batches the returned Focus cmd (see keys.go).
func newDispatchForm(cfg *config.Config) *dispatchForm {
	var rs []repos.Repo
	if cfg != nil {
		rs = repos.Discover(cfg)
	}

	filter := textinput.New()
	filter.Placeholder = "filter repos…"
	filter.CharLimit = 80

	feature := textinput.New()
	feature.Placeholder = "payment retry flow"
	feature.CharLimit = 80

	prompt := textinput.New()
	prompt.Placeholder = "describe the work to dispatch…"
	prompt.CharLimit = 500

	return &dispatchForm{step: dispatchRepo, repos: rs, filter: filter, feature: feature, prompt: prompt}
}

// filtered returns the repos matching the current filter (by name or product).
func (df *dispatchForm) filtered() []repos.Repo {
	q := strings.TrimSpace(strings.ToLower(df.filter.Value()))
	if q == "" {
		return df.repos
	}
	var out []repos.Repo
	for _, r := range df.repos {
		if strings.Contains(strings.ToLower(r.Name), q) || strings.Contains(strings.ToLower(r.Product), q) {
			out = append(out, r)
		}
	}
	return out
}

// updateDispatchForm handles keys while the new-dispatch overlay is open.
func (m model) updateDispatchForm(k string) (model, tea.Cmd) {
	df := m.dispatchForm
	if df == nil {
		return m, nil
	}
	df.errMsg = ""

	switch df.step {
	case dispatchRepo:
		switch k {
		case "esc":
			m.dispatchForm = nil
			return m, nil
		case "up", "ctrl+k":
			if df.cursor > 0 {
				df.cursor--
			}
			return m, nil
		case "down", "ctrl+j":
			if df.cursor < len(df.filtered())-1 {
				df.cursor++
			}
			return m, nil
		case "enter":
			vis := df.filtered()
			if len(vis) == 0 {
				df.errMsg = "no repo matches — esc, then , to edit scan roots"
				return m, nil
			}
			df.cursor = clampCursor(df.cursor, len(vis))
			df.repo = vis[df.cursor]
			df.step = dispatchFeature
			df.filter.Blur()
			return m, df.feature.Focus()
		default:
			prev := df.filter.Value()
			var cmd tea.Cmd
			df.filter, cmd = df.filter.Update(m.inputMsg(k))
			if df.filter.Value() != prev {
				df.cursor = 0
			}
			return m, cmd
		}

	case dispatchFeature:
		switch k {
		case "esc":
			df.step = dispatchRepo
			df.feature.Blur()
			return m, df.filter.Focus()
		case "enter":
			if strings.TrimSpace(df.feature.Value()) == "" {
				df.errMsg = "feature name is required — history is navigated by feature"
				return m, nil
			}
			df.step = dispatchPrompt
			df.feature.Blur()
			return m, df.prompt.Focus()
		default:
			var cmd tea.Cmd
			df.feature, cmd = df.feature.Update(m.inputMsg(k))
			return m, cmd
		}

	case dispatchPrompt:
		switch k {
		case "esc":
			df.step = dispatchFeature
			df.prompt.Blur()
			return m, df.feature.Focus()
		case "enter", "ctrl+d":
			if strings.TrimSpace(df.prompt.Value()) == "" {
				df.errMsg = "prompt is required"
				return m, nil
			}
			repo := df.repo.Name
			feature := strings.TrimSpace(df.feature.Value())
			prompt := strings.TrimSpace(df.prompt.Value())
			m.dispatchForm = nil
			m.notice = "dispatching \"" + feature + "\"…"
			// This overlay closes onto whichever lens was behind it, so the
			// dispatch has to be on the triage table by the time the human gets
			// there — see pending.go.
			m = m.markPending(m.pendingFor(repo, feature, prompt)).fleetSync()
			return m, launchCmd(m.cfg, repo, feature, prompt)
		default:
			var cmd tea.Cmd
			df.prompt, cmd = df.prompt.Update(m.inputMsg(k))
			return m, cmd
		}
	}
	return m, nil
}

// slugPreview shows the branch the feature name will produce, mirroring the
// real slug so the user sees feature/<slug> before dispatching.
func slugPreview(feature string) string {
	s := dispatchpkg.Slugify(feature)
	if s == "" {
		return "…"
	}
	return s
}

// window returns the [start,end) slice of an n-item list to show a `size`-row
// viewport centred on sel, clamped to the list bounds.
func window(sel, n, size int) (start, end int) {
	if size <= 0 || n == 0 {
		return 0, 0
	}
	if n <= size {
		return 0, n
	}
	start = sel - size/2
	if start < 0 {
		start = 0
	}
	end = start + size
	if end > n {
		end = n
		start = end - size
	}
	return start, end
}

// viewDispatchForm renders the new-dispatch overlay.
func (m model) viewDispatchForm(w, h int) string {
	df := m.dispatchForm
	iw := w - 2*pad
	if iw < 10 {
		iw = w
	}
	inW := iw - 4
	if inW < 10 {
		inW = 10
	}
	df.filter.Width, df.feature.Width, df.prompt.Width = inW, inW, inW

	var lines []string
	lines = append(lines, fg(cWhite, "new dispatch"))
	lines = append(lines, fg(cDim, "step "+itoa(int(df.step)+1)+" of 3 · repo → feature → prompt · esc backs out"))
	lines = append(lines, "")

	// Breadcrumb of what's already been chosen.
	if df.step > dispatchRepo {
		prod := df.repo.Product
		if prod == "" {
			prod = "—"
		}
		lines = append(lines, row(iw, "", c("repo", 10, cFaint), flexc(df.repo.Name+"  ·  "+prod, cMid)))
	}
	if df.step > dispatchFeature {
		lines = append(lines, row(iw, "", c("feature", 10, cFaint), flexc("feature/"+slugPreview(df.feature.Value()), cMid)))
	}
	if df.step > dispatchRepo {
		lines = append(lines, "")
	}

	switch df.step {
	case dispatchRepo:
		lines = append(lines, fg(cMid, "▸ ")+df.filter.View())
		lines = append(lines, "")
		vis := df.filtered()
		if len(vis) == 0 {
			lines = append(lines, blank(2)+fg(cFaint, "no repos — check scan roots in settings (,)"))
			break
		}
		sel := clampCursor(df.cursor, len(vis))
		room := h - len(lines) - 2
		if room < 1 {
			room = 1
		}
		start, end := window(sel, len(vis), room)
		for i := start; i < end; i++ {
			r := vis[i]
			bg, marker, nameColor := cTransparent, " ", cFg
			if i == sel {
				bg, marker, nameColor = cSel, "▸", cWhite
			}
			prod := r.Product
			if prod == "" {
				prod = "—"
			}
			lines = append(lines, row(iw, bg,
				c(marker, 2, cMid),
				c(r.Name, 34, nameColor),
				flexc(prod, cDim),
			))
		}

	case dispatchFeature:
		lines = append(lines, row(iw, "", c("feature", 10, cMid), flexc(df.feature.View(), cWhite)))
		lines = append(lines, "")
		lines = append(lines, blank(2)+fg(cFaint, "the branch will be ")+fg(cMid, "feature/"+slugPreview(df.feature.Value())))
		lines = append(lines, blank(2)+fg(cFaint, "enter → prompt · esc → repo"))

	case dispatchPrompt:
		lines = append(lines, fg(cFaint, "prompt")+"  "+fg(cMid, df.repo.Name)+fg(cFaint, " · ")+fg(cMid, strings.TrimSpace(df.feature.Value())))
		lines = append(lines, "")
		lines = append(lines, df.prompt.View())
		lines = append(lines, "")
		lines = append(lines, blank(2)+fg(cFaint, "enter or ctrl+d dispatches · esc → feature"))
	}

	if df.errMsg != "" {
		lines = append(lines, "", fg(cRed, "! "+df.errMsg))
	}
	return clampLines(gutter(vjoin(lines...), pad), h)
}
