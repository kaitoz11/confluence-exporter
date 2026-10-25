package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kaitoz11/confluence-exporter/internal/confluence"
)

type spaceItem struct{ s confluence.Space }

func (i spaceItem) Title() string { return i.s.Name }
func (i spaceItem) Description() string {
	kind := "Team space"
	if i.s.Type == "personal" {
		kind = "Personal space"
	}
	return "Key: " + i.s.Key + "  ·  " + kind
}
func (i spaceItem) FilterValue() string { return i.s.Name + " " + i.s.Key }

func newSpaceList(spaces []confluence.Space, width, height int) list.Model {
	items := make([]list.Item, len(spaces))
	for i, s := range spaces {
		items[i] = spaceItem{s}
	}
	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Foreground(colAccent).BorderForeground(colAccent)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(colAccent).BorderForeground(colAccent)
	l := list.New(items, d, width, height)
	l.Title = fmt.Sprintf("Choose the space to export  (%d available — press / to search)", len(spaces))
	l.Styles.Title = sHeading
	l.SetShowHelp(false)
	l.SetStatusBarItemName("space", "spaces")
	l.DisableQuitKeybindings()
	return l
}

func (m *Model) selectSpaceKey(key string) {
	if key == "" {
		return
	}
	for i, it := range m.spaceList.Items() {
		if strings.EqualFold(it.(spaceItem).s.Key, key) {
			m.spaceList.Select(i)
			return
		}
	}
}

func (m Model) updateSpaces(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && m.spaceList.FilterState() != list.Filtering {
		switch k.String() {
		case "enter":
			if it, ok := m.spaceList.SelectedItem().(spaceItem); ok {
				cmd := m.loadTree(it.s)
				return m, cmd
			}
		case "esc":
			if m.spaceList.FilterState() == list.FilterApplied {
				m.spaceList.ResetFilter()
				return m, nil
			}
			m.screen = scrLogin
			cmd := m.login.focusCmd()
			return m, cmd
		case "q":
			return m, nil // avoid quitting by accident
		}
	}
	var cmd tea.Cmd
	m.spaceList, cmd = m.spaceList.Update(msg)
	return m, cmd
}
