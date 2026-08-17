package cockpit

// dispatchform.go is the in-cockpit "new dispatch" overlay: the classic
// cockpit's repo → feature → mode → model → fan out → prompt flow, ported into
// v2 so ad-hoc work can be dispatched without a backlog ticket. Open it with
// `+` or the palette's "dispatch" / "new dispatch" command. Like settings, it
// lives behind a pointer on the model so its textinputs keep focus state
// across value-receiver Update copies. Submitting hands off to launchCmd,
// which does the real dispatch.
//
// MODE, MODEL and FAN OUT are steps of their own rather than defaults this
// overlay picks quietly. The first two reach the process as launch flags
// (--permission-mode and --model), and fan-out reaches it as the ultracode
// sentence in the prompt (see dispatch/fanout.go) — so a form that chose any
// of them on the human's behalf would be deciding that silently every time.
// Each opens on its default with the list in view, so taking the default is
// one keypress and changing it is two.

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
	dispatchMode
	dispatchModel
	dispatchFanout
	dispatchPrompt
	dispatchStepCount
)

// dispatchForm is the open new-dispatch overlay. Each step owns one textinput;
// keeping three (rather than reusing one) preserves what you typed when you esc
// back a step.
type dispatchForm struct {
	step   dispatchStep
	repos  []repos.Repo
	cursor int
	repo   repos.Repo

	filter    textinput.Model // step 1: filter repos by name/product
	feature   textinput.Model // step 2: feature name
	modeSel   int             // step 3: cursor into dispatchpkg.Modes()
	modelSel  int             // step 4: cursor into dispatchpkg.Models()
	fanoutSel int             // step 5: cursor into dispatchFanoutOptions
	prompt    textinput.Model // step 6: the prompt

	errMsg string
}

// mode is the permission mode step 3 has landed on. The cursor is the state
// and the mode is derived from it, so the two can never disagree.
func (df *dispatchForm) mode() dispatchpkg.Mode {
	all := dispatchpkg.Modes()
	return all[clampCursor(df.modeSel, len(all))]
}

// mdl is the model step 4 has landed on — same cursor-is-the-state shape.
func (df *dispatchForm) mdl() dispatchpkg.Model {
	all := dispatchpkg.Models()
	return all[clampCursor(df.modelSel, len(all))]
}

// fanOut is whether step 5 chose fanning out.
func (df *dispatchForm) fanOut() bool { return df.fanoutSel == 1 }

// dispatchFanoutOptions are FAN OUT's two answers, in offer order: staying
// solo is the default. The labels match the dx form's switch, and the hints
// say what each position actually does.
var dispatchFanoutOptions = []struct{ label, hint string }{
	{"solo", "one session does all the work itself"},
	{"fan out", "may spread across multiple agents where the task splits"},
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

	// modeSel and modelSel open on their default's own index rather than on 0,
	// so the offer order in dispatchpkg can change without silently changing
	// what a dispatch that took the default runs as.
	sel := 0
	for i, k := range dispatchpkg.Modes() {
		if k == dispatchpkg.DefaultMode {
			sel = i
		}
	}
	mdlSel := 0
	for i, k := range dispatchpkg.Models() {
		if k == dispatchpkg.DefaultModel {
			mdlSel = i
		}
	}
	return &dispatchForm{step: dispatchRepo, repos: rs, modeSel: sel, modelSel: mdlSel, filter: filter, feature: feature, prompt: prompt}
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
			df.step = dispatchMode
			df.feature.Blur()
			return m, nil
		default:
			var cmd tea.Cmd
			df.feature, cmd = df.feature.Update(m.inputMsg(k))
			return m, cmd
		}

	case dispatchMode:
		// Nothing on this step types, so every key is navigation and anything
		// unrecognised is swallowed rather than treated as text. The same goes
		// for the model and fan-out steps below.
		switch k {
		case "esc":
			df.step = dispatchFeature
			return m, df.feature.Focus()
		case "up", "ctrl+k", "left":
			if df.modeSel > 0 {
				df.modeSel--
			}
			return m, nil
		case "down", "ctrl+j", "right":
			if df.modeSel < len(dispatchpkg.Modes())-1 {
				df.modeSel++
			}
			return m, nil
		case "enter":
			df.step = dispatchModel
			return m, nil
		}
		return m, nil

	case dispatchModel:
		switch k {
		case "esc":
			df.step = dispatchMode
			return m, nil
		case "up", "ctrl+k", "left":
			if df.modelSel > 0 {
				df.modelSel--
			}
			return m, nil
		case "down", "ctrl+j", "right":
			if df.modelSel < len(dispatchpkg.Models())-1 {
				df.modelSel++
			}
			return m, nil
		case "enter":
			df.step = dispatchFanout
			return m, nil
		}
		return m, nil

	case dispatchFanout:
		switch k {
		case "esc":
			df.step = dispatchModel
			return m, nil
		case "up", "ctrl+k", "left":
			if df.fanoutSel > 0 {
				df.fanoutSel--
			}
			return m, nil
		case "down", "ctrl+j", "right":
			if df.fanoutSel < len(dispatchFanoutOptions)-1 {
				df.fanoutSel++
			}
			return m, nil
		case "enter":
			df.step = dispatchPrompt
			return m, df.prompt.Focus()
		}
		return m, nil

	case dispatchPrompt:
		switch k {
		case "esc":
			df.step = dispatchFanout
			df.prompt.Blur()
			return m, nil
		case "enter", "ctrl+d":
			if strings.TrimSpace(df.prompt.Value()) == "" {
				df.errMsg = "prompt is required"
				return m, nil
			}
			repo := df.repo.Name
			feature := strings.TrimSpace(df.feature.Value())
			prompt := strings.TrimSpace(df.prompt.Value())
			mode := df.mode()
			mdl := df.mdl()
			fanOut := df.fanOut()
			m.dispatchForm = nil
			notice := "dispatching \"" + feature + "\" · " + string(mode)
			if mdl != dispatchpkg.DefaultModel {
				notice += " · " + string(mdl)
			}
			if fanOut {
				notice += " · fans out"
			}
			m.notice = notice + "…"
			// This overlay closes onto whichever lens was behind it, so the
			// dispatch has to be on the triage table by the time the human gets
			// there — see pending.go.
			m = m.markPending(m.pendingFor(repo, feature, prompt)).fleetSync()
			return m, launchCmd(m.cfg, repo, feature, prompt, mode, mdl, fanOut)
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
	lines = append(lines, fg(cDim, "step "+itoa(int(df.step)+1)+" of "+itoa(int(dispatchStepCount))+
		" · repo → feature → mode → model → fan out → prompt · esc backs out"))
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
	if df.step > dispatchMode {
		lines = append(lines, row(iw, "", c("mode", 10, cFaint), flexc(string(df.mode()), cMid)))
	}
	if df.step > dispatchModel {
		lines = append(lines, row(iw, "", c("model", 10, cFaint), flexc(string(df.mdl()), cMid)))
	}
	if df.step > dispatchFanout {
		lines = append(lines, row(iw, "", c("fan out", 10, cFaint), flexc(dispatchFanoutOptions[clampCursor(df.fanoutSel, len(dispatchFanoutOptions))].label, cMid)))
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
		lines = append(lines, blank(2)+fg(cFaint, "enter → mode · esc → repo"))

	case dispatchMode:
		lines = append(lines, fg(cMid, "mode")+fg(cFaint, "  what the session may do without asking"))
		lines = append(lines, "")
		all := dispatchpkg.Modes()
		sel := clampCursor(df.modeSel, len(all))
		for i, k := range all {
			bg, marker, nameColor := cTransparent, " ", cFg
			if i == sel {
				bg, marker, nameColor = cSel, "▸", cWhite
			}
			lines = append(lines, row(iw, bg,
				c(marker, 2, cMid),
				c(string(k), 10, nameColor),
				flexc(k.Hint(), cDim),
			))
		}
		lines = append(lines, "")
		lines = append(lines, blank(2)+fg(cFaint, "enter → model · esc → feature"))

	case dispatchModel:
		lines = append(lines, fg(cMid, "model")+fg(cFaint, "  what the session runs — default passes no flag"))
		lines = append(lines, "")
		all := dispatchpkg.Models()
		sel := clampCursor(df.modelSel, len(all))
		for i, k := range all {
			bg, marker, nameColor := cTransparent, " ", cFg
			if i == sel {
				bg, marker, nameColor = cSel, "▸", cWhite
			}
			lines = append(lines, row(iw, bg,
				c(marker, 2, cMid),
				c(string(k), 10, nameColor),
				flexc(k.Hint(), cDim),
			))
		}
		lines = append(lines, "")
		lines = append(lines, blank(2)+fg(cFaint, "enter → fan out · esc → mode"))

	case dispatchFanout:
		lines = append(lines, fg(cMid, "fan out")+fg(cFaint, "  may it spread across agents when the task splits"))
		lines = append(lines, "")
		sel := clampCursor(df.fanoutSel, len(dispatchFanoutOptions))
		for i, opt := range dispatchFanoutOptions {
			bg, marker, nameColor := cTransparent, " ", cFg
			if i == sel {
				bg, marker, nameColor = cSel, "▸", cWhite
			}
			lines = append(lines, row(iw, bg,
				c(marker, 2, cMid),
				c(opt.label, 10, nameColor),
				flexc(opt.hint, cDim),
			))
		}
		lines = append(lines, "")
		lines = append(lines, blank(2)+fg(cFaint, "enter → prompt · esc → model"))

	case dispatchPrompt:
		lines = append(lines, fg(cFaint, "prompt")+"  "+fg(cMid, df.repo.Name)+fg(cFaint, " · ")+fg(cMid, strings.TrimSpace(df.feature.Value())))
		lines = append(lines, "")
		lines = append(lines, df.prompt.View())
		lines = append(lines, "")
		lines = append(lines, blank(2)+fg(cFaint, "enter or ctrl+d dispatches · esc → fan out"))
	}

	if df.errMsg != "" {
		lines = append(lines, "", fg(cRed, "! "+df.errMsg))
	}
	return clampLines(gutter(vjoin(lines...), pad), h)
}
