package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kaitoz11/confluence-exporter/internal/config"
	"github.com/kaitoz11/confluence-exporter/internal/confluence"
)

const (
	fieldURL = iota
	fieldEmail
	fieldToken
	fieldRemember
	fieldConnect
	loginFields
)

type loginModel struct {
	inputs   []textinput.Model
	focus    int
	remember bool
	err      string
}

func newLogin(cfg config.Config) loginModel {
	mk := func(placeholder, value string) textinput.Model {
		t := textinput.New()
		t.Placeholder = placeholder
		t.SetValue(value)
		t.CharLimit = 512
		t.Width = 50
		t.Prompt = ""
		return t
	}
	l := loginModel{
		inputs: []textinput.Model{
			mk("yourcompany.atlassian.net", cfg.URL),
			mk("you@company.com", cfg.Email),
			mk("paste your API token here", cfg.Token),
		},
		remember: cfg.Token != "",
	}
	l.inputs[fieldToken].EchoMode = textinput.EchoPassword
	l.inputs[fieldToken].EchoCharacter = '•'
	l.syncPlaceholders()
	// Start on the first required field that is still empty.
	site, ok := l.site()
	switch {
	case !ok:
		l.focus = fieldURL
	case site.Cloud && cfg.Email == "":
		l.focus = fieldEmail
	case cfg.Token == "":
		l.focus = fieldToken
	default:
		l.focus = fieldConnect
	}
	return l
}

func (l *loginModel) focusCmd() tea.Cmd {
	var cmd tea.Cmd
	for i := range l.inputs {
		if i == l.focus {
			cmd = l.inputs[i].Focus()
		} else {
			l.inputs[i].Blur()
		}
	}
	return cmd
}

func (l loginModel) site() (confluence.Site, bool) {
	s, err := confluence.ParseSiteURL(l.inputs[fieldURL].Value())
	return s, err == nil
}

func (m Model) updateLogin(msg tea.Msg) (tea.Model, tea.Cmd) {
	l := &m.login
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			m.screen = scrMenu
			return m, nil
		case "tab", "down":
			l.focus = (l.focus + 1) % loginFields
			cmd := l.focusCmd()
			return m, cmd
		case "shift+tab", "up":
			l.focus = (l.focus + loginFields - 1) % loginFields
			cmd := l.focusCmd()
			return m, cmd
		case "ctrl+r":
			if l.inputs[fieldToken].EchoMode == textinput.EchoPassword {
				l.inputs[fieldToken].EchoMode = textinput.EchoNormal
			} else {
				l.inputs[fieldToken].EchoMode = textinput.EchoPassword
			}
			return m, nil
		case " ":
			if l.focus == fieldRemember {
				l.remember = !l.remember
				return m, nil
			}
			if l.focus == fieldConnect {
				return m.submitLogin()
			}
		case "enter":
			switch l.focus {
			case fieldRemember, fieldConnect:
				return m.submitLogin()
			default:
				// Enter on a field moves on, or connects once everything is filled.
				if l.complete() {
					return m.submitLogin()
				}
				l.focus++
				cmd := l.focusCmd()
				return m, cmd
			}
		}
	}
	if l.focus < len(l.inputs) {
		var cmd tea.Cmd
		l.inputs[l.focus], cmd = l.inputs[l.focus].Update(msg)
		l.syncPlaceholders()
		if _, isKey := msg.(tea.KeyMsg); isKey {
			l.err = ""
		}
		return m, cmd
	}
	return m, nil
}

func (l loginModel) complete() bool {
	site, ok := l.site()
	if !ok || strings.TrimSpace(l.inputs[fieldToken].Value()) == "" {
		return false
	}
	return !site.Cloud || strings.TrimSpace(l.inputs[fieldEmail].Value()) != ""
}

func (m Model) submitLogin() (tea.Model, tea.Cmd) {
	l := &m.login
	site, ok := l.site()
	switch {
	case !ok:
		l.err = "Please enter your Confluence address, for example yourcompany.atlassian.net"
		l.focus = fieldURL
	case site.Cloud && strings.TrimSpace(l.inputs[fieldEmail].Value()) == "":
		l.err = "Confluence Cloud needs the email address you sign in with."
		l.focus = fieldEmail
	case strings.TrimSpace(l.inputs[fieldToken].Value()) == "":
		l.err = "Please paste your API token. Press esc and choose Help if you don't have one yet."
		l.focus = fieldToken
	default:
		cmd := m.connect()
		return m, cmd
	}
	cmd := l.focusCmd()
	return m, cmd
}

func (m Model) viewLogin() string {
	l := m.login
	site, ok := l.site()
	var b strings.Builder
	b.WriteString(sHeading.Render("Connect to Confluence") + "\n")
	b.WriteString(sMuted.Render("Your details stay on this computer and are only sent to your Confluence site.") + "\n\n")

	field := func(i int, label, hint string) {
		b.WriteString(inputRow(label, l.inputs[i].View(), l.focus == i) + "\n")
		if hint != "" {
			b.WriteString(hintIndent + hint + "\n")
		}
	}

	urlHint := sMuted.Render("Tip: you can paste any link copied from your browser")
	if ok && strings.TrimSpace(l.inputs[fieldURL].Value()) != "" {
		if site.Cloud {
			urlHint = sSuccess.Render("✓ Confluence Cloud")
		} else {
			urlHint = sSuccess.Render("✓ Confluence Server / Data Center")
		}
		if site.SpaceKey != "" {
			urlHint += sMuted.Render("  · space " + site.SpaceKey + " pre-selected")
		}
	}
	field(fieldURL, "Confluence address", urlHint)

	emailLabel, emailHint := "Email", sMuted.Render("The email you use to sign in to Atlassian")
	tokenHint := sMuted.Render("Create one at ") + sKey.Render("id.atlassian.com/manage-profile/security/api-tokens")
	if ok && !site.Cloud {
		emailLabel = "Username (optional)"
		emailHint = sMuted.Render("Leave empty when using a personal access token")
		tokenHint = sMuted.Render("Profile picture → Settings → Personal Access Tokens")
	}
	field(fieldEmail, emailLabel, emailHint)
	field(fieldToken, "API token", tokenHint)
	b.WriteString("\n")
	b.WriteString(checkbox("Remember my token on this computer", l.remember, l.focus == fieldRemember) + "\n\n")
	b.WriteString(button("Connect", l.focus == fieldConnect) + "\n")
	if l.err != "" {
		b.WriteString("\n  " + sError.Render("✗ "+l.err) + "\n")
	}
	return b.String()
}

// syncPlaceholders adapts the email field to the kind of site.
func (l *loginModel) syncPlaceholders() {
	if site, ok := l.site(); ok && !site.Cloud && strings.TrimSpace(l.inputs[fieldURL].Value()) != "" {
		l.inputs[fieldEmail].Placeholder = "leave empty with a personal access token"
		l.inputs[fieldToken].Placeholder = "paste your personal access token here"
	} else {
		l.inputs[fieldEmail].Placeholder = "you@company.com"
		l.inputs[fieldToken].Placeholder = "paste your API token here"
	}
}
