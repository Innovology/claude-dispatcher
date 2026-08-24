package cockpit

// dispatchform.go is the in-cockpit "new dispatch" overlay: the classic
// cockpit's repo → feature → root → mode → model → fan out → prompt flow,
// ported into v2 so ad-hoc work can be dispatched without a backlog ticket.
// Open it with `+` or the palette's "dispatch" / "new dispatch" command. Like
// settings, it lives behind a pointer on the model so its textinputs keep focus
// state across value-receiver Update copies. Submitting hands off to launchCmd,
// which does the real dispatch.
//
// ROOT, MODE, MODEL and FAN OUT are steps of their own rather than defaults
// this overlay picks quietly. MODE and MODEL reach the process as launch flags
// (--permission-mode and --model), fan-out reaches it as the ultracode sentence
// in the prompt (see dispatch/fanout.go), and ROOT is the branch the work is
// cut from — so a form that chose any of them on the human's behalf would be
// deciding that silently every time. Each opens on its default with the list in
// view, so taking the default is one keypress and changing it is two.
//
// ROOT is a filtered list rather than a switch because it is the only one of
// the four whose choices are the repo's and not the product's: the repos this
// form dispatches into carry 170-odd branches each, which is a list you type at
// and not one you cycle through. Its first row is "default", which names no
// branch at all — the repo's default is resolved from the remote at launch, and
// a form that filled it in here would be quoting the same stale local cache
// that made dispatches fork dead branches (see dispatch/root.go).

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
	dispatchRoot
	dispatchMode
	dispatchModel
	dispatchFanout
	dispatchPrompt
	dispatchStepCount
)

// dispatchForm is the open new-dispatch overlay. Each step owns one textinput;
// keeping them separate (rather than reusing one) preserves what you typed when
// you esc back a step.
type dispatchForm struct {
	step   dispatchStep
	repos  []repos.Repo
	cursor int
	repo   repos.Repo

	filter     textinput.Model // step 1: filter repos by name/product
	feature    textinput.Model // step 2: feature name
	rootFilter textinput.Model // step 3: filter the picked repo's branches
	// roots are the picked repo's branches, read once when the repo is chosen
	// rather than per keystroke: this is a git call, and the list cannot change
	// under a human who is looking at it.
	rootSel   int             // step 3: cursor into rootOptions()
	roots     []dispatchpkg.RootChoice
	modeSel   int             // step 4: cursor into dispatchpkg.Modes()
	modelSel  int             // step 5: cursor into dispatchpkg.Models()
	fanoutSel int             // step 6: cursor into dispatchFanoutOptions
	prompt    textinput.Model // step 7: the prompt

	errMsg string
}

// rootOption is one row of the ROOT step: the default, then a branch each.
type rootOption struct {
	name  string // the branch, or "default"
	where string // "origin" / "local", blank for the default row
	hint  string
}

// rootOptions is every row the ROOT step could show, in order. The default
// leads: it is the right answer for nearly every dispatch, and it is the only
// one that cannot be a dead branch, because it is resolved from the remote at
// launch instead of read from anything local.
func (df *dispatchForm) rootOptions() []rootOption {
	out := []rootOption{{
		name: string(dispatchpkg.RootDefault),
		hint: dispatchpkg.RootDefault.Hint(),
	}}
	for _, b := range df.roots {
		hint := "last commit " + usgAgo(b.When) + " ago"
		if b.Where == "local" {
			// Worth saying out loud: a branch only this clone has is one the PR
			// will be opened against a base that origin has never seen.
			hint = "local only · " + hint
		}
		out = append(out, rootOption{name: b.Name, where: b.Where, hint: hint})
	}
	return out
}

// rootFiltered is rootOptions narrowed by what has been typed. The default row
// filters like any other — typing "def" finds it — because a row that survived
// every filter would be a row the human could not get rid of while looking for
// the branch they actually want.
func (df *dispatchForm) rootFiltered() []rootOption {
	q := strings.TrimSpace(strings.ToLower(df.rootFilter.Value()))
	all := df.rootOptions()
	if q == "" {
		return all
	}
	var out []rootOption
	for _, o := range all {
		if strings.Contains(strings.ToLower(o.name), q) {
			out = append(out, o)
		}
	}
	return out
}

// root is the branch step 3 has landed on, as the launch takes it.
func (df *dispatchForm) root() dispatchpkg.Root {
	vis := df.rootFiltered()
	if len(vis) == 0 {
		return dispatchpkg.RootDefault
	}
	return dispatchpkg.Root(vis[clampCursor(df.rootSel, len(vis))].name).Normalize()
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

	rootFilter := textinput.New()
	rootFilter.Placeholder = "filter branches…"
	rootFilter.CharLimit = 120 // a branch name, not a sentence

	prompt := textinput.New()
	prompt.Placeholder = "describe the work to dispatch…"
	// No limit. A textinput silently drops everything past CharLimit, and at
	// 500 that is most of any brief worth pasting: the human watched their
	// prompt go in whole and dispatched a third of it, with nothing on screen
	// saying so. The dispatch itself is what bounds a prompt now
	// (dispatch.MaxPromptBytes), and it says so out loud when it refuses.
	prompt.CharLimit = 0

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
	return &dispatchForm{step: dispatchRepo, repos: rs, modeSel: sel, modelSel: mdlSel,
		filter: filter, feature: feature, rootFilter: rootFilter, prompt: prompt}
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

// pickRepo lands the repo step on r and reads that repo's branches for the
// ROOT step. Coming back and choosing a different repo resets the branch
// choice with them: a root is a branch in one repo, and carrying "release/24"
// across to a repo that has no such branch would be carrying a launch failure.
func (df *dispatchForm) pickRepo(r repos.Repo) {
	if df.repo.Path == r.Path && df.roots != nil {
		return
	}
	df.repo = r
	df.roots = dispatchpkg.RootBranches(r.Path)
	df.rootSel = 0
	df.rootFilter.SetValue("")
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
			df.pickRepo(vis[df.cursor])
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
			df.step = dispatchRoot
			df.feature.Blur()
			return m, df.rootFilter.Focus()
		default:
			var cmd tea.Cmd
			df.feature, cmd = df.feature.Update(m.inputMsg(k))
			return m, cmd
		}

	case dispatchRoot:
		switch k {
		case "esc":
			df.step = dispatchFeature
			df.rootFilter.Blur()
			return m, df.feature.Focus()
		case "up", "ctrl+k":
			if df.rootSel > 0 {
				df.rootSel--
			}
			return m, nil
		case "down", "ctrl+j":
			if df.rootSel < len(df.rootFiltered())-1 {
				df.rootSel++
			}
			return m, nil
		case "enter":
			if len(df.rootFiltered()) == 0 {
				df.errMsg = "no branch matches — backspace to widen, or clear it for the default"
				return m, nil
			}
			df.step = dispatchMode
			df.rootFilter.Blur()
			return m, nil
		default:
			prev := df.rootFilter.Value()
			var cmd tea.Cmd
			df.rootFilter, cmd = df.rootFilter.Update(m.inputMsg(k))
			if df.rootFilter.Value() != prev {
				// A narrowed list is a different list: the cursor goes home
				// rather than pointing at whatever now sits at that index.
				df.rootSel = 0
			}
			return m, cmd
		}

	case dispatchMode:
		// Nothing on this step types, so every key is navigation and anything
		// unrecognised is swallowed rather than treated as text. The same goes
		// for the model and fan-out steps below.
		switch k {
		case "esc":
			df.step = dispatchRoot
			return m, df.rootFilter.Focus()
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
			root := df.root()
			fanOut := df.fanOut()
			m.dispatchForm = nil
			notice := "dispatching \"" + feature + "\" · " + string(mode)
			if mdl != dispatchpkg.DefaultModel {
				notice += " · " + string(mdl)
			}
			// Named only when a branch was named. "default" is the absence of a
			// choice, and the branch it resolves to is not known until the launch
			// asks the remote — printing one here would be a guess on the line
			// that reports what happened.
			if !root.IsDefault() {
				notice += " · from " + string(root)
			}
			if fanOut {
				notice += " · fans out"
			}
			m.notice = notice + "…"
			// This overlay closes onto whichever lens was behind it, so the
			// dispatch has to be on the triage table by the time the human gets
			// there — see pending.go.
			m = m.markPending(m.pendingFor(repo, feature, prompt)).fleetSync()
			return m, launchCmd(m.cfg, repo, feature, prompt, mode, mdl, root, fanOut)
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
	df.rootFilter.Width = inW

	var lines []string
	lines = append(lines, fg(cWhite, "new dispatch"))
	lines = append(lines, fg(cDim, "step "+itoa(int(df.step)+1)+" of "+itoa(int(dispatchStepCount))+
		" · repo → feature → root → mode → model → fan out → prompt · esc backs out"))
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
	if df.step > dispatchRoot {
		lines = append(lines, row(iw, "", c("root", 10, cFaint), flexc(string(df.root()), cMid)))
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
		lines = append(lines, blank(2)+fg(cFaint, "enter → root · esc → repo"))

	case dispatchRoot:
		lines = append(lines, fg(cMid, "root")+fg(cFaint, "  the branch this work is cut from"))
		lines = append(lines, "")
		lines = append(lines, fg(cMid, "▸ ")+df.rootFilter.View())
		lines = append(lines, "")
		vis := df.rootFiltered()
		if len(vis) == 0 {
			lines = append(lines, blank(2)+fg(cFaint, "no branch matches — backspace to widen"))
			break
		}
		sel := clampCursor(df.rootSel, len(vis))
		// One row reserved for the closing hint, which says which key goes on:
		// on a repo with 170 branches the list would otherwise run to the floor
		// and take the only line that says how to leave it.
		room := h - len(lines) - 3
		if room < 1 {
			room = 1
		}
		start, end := window(sel, len(vis), room)
		for i := start; i < end; i++ {
			o := vis[i]
			bg, marker, nameColor := cTransparent, " ", cFg
			if i == sel {
				bg, marker, nameColor = cSel, "▸", cWhite
			}
			lines = append(lines, row(iw, bg,
				c(marker, 2, cMid),
				c(o.name, 34, nameColor),
				flexc(o.hint, cDim),
			))
		}
		lines = append(lines, "")
		lines = append(lines, blank(2)+fg(cFaint, "enter → mode · esc → feature"))

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
		lines = append(lines, blank(2)+fg(cFaint, "enter → model · esc → root"))

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
