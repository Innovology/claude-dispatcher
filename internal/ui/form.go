package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/repos"
)

// form is the three-step dispatch flow: pick repo → name feature → write prompt.
type formResult int

const (
	formActive formResult = iota
	formCancelled
	formSubmitted
)

type formStep int

const (
	stepRepo formStep = iota
	stepFeature
	stepPrompt
)

type form struct {
	step    formStep
	repos   []repos.Repo
	filter  string
	cursor  int
	repo    repos.Repo
	feature textinput.Model
	prompt  textarea.Model
	errMsg  string
}

func newForm(rs []repos.Repo) *form {
	feature := textinput.New()
	feature.Placeholder = "payment retry flow"
	feature.CharLimit = 80

	prompt := textarea.New()
	prompt.Placeholder = "Describe the work to dispatch…"

	return &form{repos: groupRepos(rs), feature: feature, prompt: prompt}
}

func (f *form) resize(width, height int) {
	w := min(width-8, 100)
	f.feature.Width = w
	f.prompt.SetWidth(w)
	f.prompt.SetHeight(max(4, min(height-14, 12)))
}

func (f *form) filtered() []repos.Repo {
	if f.filter == "" {
		return f.repos
	}
	var out []repos.Repo
	needle := strings.ToLower(f.filter)
	for _, r := range f.repos {
		if strings.Contains(strings.ToLower(r.Name), needle) {
			out = append(out, r)
		}
	}
	return out
}

func (f *form) featureName() string { return strings.TrimSpace(f.feature.Value()) }
func (f *form) promptText() string  { return strings.TrimSpace(f.prompt.Value()) }

func (f *form) update(msg tea.KeyMsg) (formResult, tea.Cmd) {
	f.errMsg = ""
	switch f.step {
	case stepRepo:
		return f.updateRepoStep(msg)
	case stepFeature:
		switch msg.String() {
		case "esc":
			f.step = stepRepo
			return formActive, nil
		case "enter":
			if f.featureName() == "" {
				f.errMsg = "feature name is required — history is navigated by feature"
				return formActive, nil
			}
			f.step = stepPrompt
			f.feature.Blur()
			return formActive, f.prompt.Focus()
		}
		var cmd tea.Cmd
		f.feature, cmd = f.feature.Update(msg)
		return formActive, cmd
	case stepPrompt:
		switch msg.String() {
		case "esc":
			f.step = stepFeature
			f.prompt.Blur()
			return formActive, f.feature.Focus()
		case "ctrl+d":
			if f.promptText() == "" {
				f.errMsg = "prompt is required"
				return formActive, nil
			}
			return formSubmitted, nil
		}
		var cmd tea.Cmd
		f.prompt, cmd = f.prompt.Update(msg)
		return formActive, cmd
	}
	return formActive, nil
}

func (f *form) updateRepoStep(msg tea.KeyMsg) (formResult, tea.Cmd) {
	visible := f.filtered()
	switch msg.String() {
	case "esc":
		return formCancelled, nil
	case "up":
		if f.cursor > 0 {
			f.cursor--
		}
		return formActive, nil
	case "down":
		if f.cursor < len(visible)-1 {
			f.cursor++
		}
		return formActive, nil
	case "enter":
		if len(visible) == 0 {
			f.errMsg = "no repo matches — check roots in config"
			return formActive, nil
		}
		f.cursor = min(f.cursor, len(visible)-1)
		f.repo = visible[f.cursor]
		f.step = stepFeature
		return formActive, f.feature.Focus()
	case "backspace":
		if f.filter != "" {
			f.filter = f.filter[:len(f.filter)-1]
			f.cursor = 0
		}
		return formActive, nil
	}
	if msg.Type == tea.KeyRunes && !msg.Alt {
		f.filter += string(msg.Runes)
		f.cursor = 0
	}
	return formActive, nil
}
