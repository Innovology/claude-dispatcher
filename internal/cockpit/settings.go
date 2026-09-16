package cockpit

// settings.go is the in-app settings editor (open with `,` or `:settings`). It
// edits the same config.toml the classic cockpit writes — scan roots plus the
// v2 integration settings (Linear key, Azure org/project) and the weekly token
// budget the usage lens needs (no API exposes the real subscription limit, so
// it is a user setting). Committing a field writes config.toml immediately and
// triggers a reload.

import (
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
)

type settingsKind int

const (
	setString settingsKind = iota
	setSecret
	setInt
	setRoots
	// setChoice is a field with a fixed set of answers: enter steps to the next
	// one and saves it, because a typed "lihgt" is a mistake the editor should
	// not be able to accept.
	setChoice
)

type settingsField struct {
	key   string // config field key
	label string
	hint  string
	kind  settingsKind
}

// settingsState is the open settings editor. It lives behind a pointer on the
// model so its textinput keeps focus state across Update copies.
type settingsState struct {
	fields  []settingsField
	cursor  int
	editing bool
	input   textinput.Model
	saved   string // transient "saved" flash
}

var settingsFields = []settingsField{
	{key: "roots", label: "scan roots", hint: "comma-separated dirs scanned for repos", kind: setRoots},
	{key: "linear_api_key", label: "Linear API key", hint: "the unscoped Linear read · a token per product on 2, a, l", kind: setSecret},
	{key: "azure_org", label: "Azure DevOps org", hint: "org URL, e.g. https://dev.azure.com/acme", kind: setString},
	{key: "azure_project", label: "Azure project", hint: "Azure Boards project name", kind: setString},
	{key: "weekly_token_limit", label: "weekly token budget", hint: "0 = unknown → usage shows raw tokens", kind: setInt},
	{key: "theme", label: "theme", hint: "enter cycles · system follows your light/dark switch while open", kind: setChoice},
}

func newSettings(cfg *config.Config) *settingsState {
	ti := textinput.New()
	ti.CharLimit = 300
	return &settingsState{fields: settingsFields, input: ti}
}

// valueFor reads the current config value for a field as a display string.
func settingsValue(cfg *config.Config, f settingsField) string {
	if cfg == nil {
		return ""
	}
	switch f.key {
	case "roots":
		return strings.Join(cfg.Roots, ", ")
	case "linear_api_key":
		return cfg.LinearAPIKey
	case "azure_org":
		return cfg.AzureOrg
	case "azure_project":
		return cfg.AzureProject
	case "weekly_token_limit":
		if cfg.WeeklyTokenLimit == 0 {
			return ""
		}
		return strconv.Itoa(cfg.WeeklyTokenLimit)
	case "theme":
		mode, _ := themeModeOf(cfg.Theme)
		return mode
	}
	return ""
}

// commit writes the edited value back into cfg for the given field.
func settingsCommit(cfg *config.Config, f settingsField, val string) {
	val = strings.TrimSpace(val)
	switch f.key {
	case "roots":
		var out []string
		for _, r := range strings.Split(val, ",") {
			if r = strings.TrimSpace(r); r != "" {
				out = append(out, r)
			}
		}
		cfg.Roots = out
	case "linear_api_key":
		cfg.LinearAPIKey = val
	case "azure_org":
		cfg.AzureOrg = val
	case "azure_project":
		cfg.AzureProject = val
	case "weekly_token_limit":
		n, _ := strconv.Atoi(val)
		cfg.WeeklyTokenLimit = n
	}
}

// updateSettings handles keys while the settings editor is open.
func (m model) updateSettings(k string) (model, tea.Cmd) {
	st := m.settings
	if st == nil {
		return m, nil
	}
	if st.editing {
		switch k {
		case "esc":
			st.editing = false
			st.input.Blur()
			return m, nil
		case "enter":
			if m.cfg != nil {
				settingsCommit(m.cfg, st.fields[st.cursor], st.input.Value())
				if err := config.Save(m.cfg); err != nil {
					m.notice = "save failed: " + err.Error()
				} else {
					applyConfigEnv(m.cfg)
					st.saved = st.fields[st.cursor].label + " saved"
					m.notice = "settings saved"
				}
			}
			st.editing = false
			st.input.Blur()
			return m.requestLoad(loadPlain)
		default:
			var cmd tea.Cmd
			st.input, cmd = st.input.Update(m.inputMsg(k))
			return m, cmd
		}
	}
	switch k {
	case "esc", "q", ",":
		m.settings = nil
	case "j", "down":
		if st.cursor < len(st.fields)-1 {
			st.cursor++
		}
		st.saved = ""
	case "k", "up":
		if st.cursor > 0 {
			st.cursor--
		}
		st.saved = ""
	case "enter":
		f := st.fields[st.cursor]
		if f.kind == setChoice {
			return m.cycleTheme()
		}
		st.input.SetValue(settingsValue(m.cfg, f))
		st.input.CursorEnd()
		st.editing = true
		st.saved = ""
		return m, st.input.Focus()
	}
	return m, nil
}

// cycleTheme steps the theme to the next mode, redraws in it at once and saves
// it. Nothing is reloaded: a theme changes how the snapshot is drawn, not what
// is in it.
func (m model) cycleTheme() (model, tea.Cmd) {
	modes := themeModes()
	next := modes[0]
	if i := slices.Index(modes, m.themeMode); i >= 0 {
		next = modes[(i+1)%len(modes)]
	}
	m.themeMode = next
	m.applyTheme()
	if m.cfg != nil {
		m.cfg.Theme = next
		if err := config.Save(m.cfg); err != nil {
			m.notice = "save failed: " + err.Error()
		} else {
			m.settings.saved = "theme saved"
		}
	}
	if next == themeSystem {
		return m.followSystem()
	}
	return m, nil
}

// keyToMsg rebuilds a tea.KeyMsg from a key string for the textinput, which
// consumes KeyMsgs. Handles the common editing keys.
func keyToMsg(k string) tea.Msg {
	switch k {
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	}
	if len([]rune(k)) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{}}
}

// viewSettings renders the settings overlay.
func (m model) viewSettings(w, h int) string {
	st := m.settings
	iw := w - 2*pad
	var lines []string
	lines = append(lines, fg(cWhite, "settings"))
	lines = append(lines, fg(cDim, "config.toml · enter edits · esc closes · env vars override secrets"))
	lines = append(lines, "")
	for i, f := range st.fields {
		marker := "  "
		labelColor := cFg
		if i == st.cursor {
			marker = fg(cMid, "▸ ")
			labelColor = cWhite
		}
		val := settingsValue(m.cfg, f)
		display := val
		switch {
		case f.kind == setSecret && val != "":
			display = "•••••••• (set)"
		case val == "":
			display = fg(cFaint, "— not set")
		case f.key == "theme" && m.themeMode == themeSystem:
			// What system means right now is the thing worth knowing about it.
			display = val + fg(cFaint, " · "+m.currentTheme().name+" now")
		}
		if st.editing && i == st.cursor {
			display = st.input.View()
		}
		lines = append(lines, marker+row(iw-2, "",
			c(f.label, 22, labelColor),
			flexc(display, cMid),
		))
		lines = append(lines, blank(2)+"  "+fg(cFaint, f.hint))
	}
	if st.saved != "" {
		lines = append(lines, "", fg(cGreen, "✓ "+st.saved))
	}
	return clampLines(gutter(vjoin(lines...), pad), h)
}
