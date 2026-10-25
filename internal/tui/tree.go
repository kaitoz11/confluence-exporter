package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kaitoz11/confluence-exporter/internal/export"
)

// treeModel shows the space's page tree with a tick box per page.
type treeModel struct {
	tree    *export.Tree
	visible []*export.Node
	cursor  int
	offset  int
	height  int
}

func newTreeModel(t *export.Tree, height int) treeModel {
	tm := treeModel{tree: t}
	tm.setHeight(height)
	tm.refresh()
	return tm
}

func (t *treeModel) setHeight(h int) {
	t.height = max(h, 3)
	t.scroll()
}

func (t *treeModel) refresh() {
	if t.tree == nil {
		return
	}
	t.visible = t.visible[:0]
	t.tree.Walk(func(n *export.Node) bool {
		t.visible = append(t.visible, n)
		return n.Expanded
	})
	t.cursor = min(t.cursor, max(len(t.visible)-1, 0))
	t.scroll()
}

func (t *treeModel) scroll() {
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+t.height {
		t.offset = t.cursor - t.height + 1
	}
	t.offset = max(t.offset, 0)
}

func (t *treeModel) move(d int) {
	t.cursor = min(max(t.cursor+d, 0), max(len(t.visible)-1, 0))
	t.scroll()
}

func (t *treeModel) current() *export.Node {
	if t.cursor < len(t.visible) {
		return t.visible[t.cursor]
	}
	return nil
}

func (t *treeModel) setAll(fn func(n *export.Node)) {
	t.tree.Walk(func(n *export.Node) bool {
		fn(n)
		return true
	})
	t.refresh()
}

func (m Model) updateTree(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	t := &m.treeView
	n := t.current()
	switch k.String() {
	case "up", "k":
		t.move(-1)
	case "down", "j":
		t.move(1)
	case "pgup":
		t.move(-t.height)
	case "pgdown":
		t.move(t.height)
	case "home", "g":
		t.move(-len(t.visible))
	case "end", "G":
		t.move(len(t.visible))
	case "right", "l":
		if n != nil && len(n.Children) > 0 {
			if n.Expanded {
				t.move(1)
			} else {
				n.Expanded = true
				t.refresh()
			}
		}
	case "left", "h":
		if n != nil {
			if n.Expanded && len(n.Children) > 0 {
				n.Expanded = false
				t.refresh()
			} else if n.Parent != nil {
				for i, v := range t.visible {
					if v == n.Parent {
						t.cursor = i
						t.scroll()
					}
				}
			}
		}
	case " ", "x":
		if n != nil {
			n.SetSelected(!(n.Selected && fullySelected(n)))
			// Ticking a page also ticks its parents, so it keeps its place.
			if n.Selected {
				for p := n.Parent; p != nil; p = p.Parent {
					p.Selected = true
				}
			}
		}
	case "a":
		t.setAll(func(n *export.Node) { n.Selected = true })
	case "n":
		t.setAll(func(n *export.Node) { n.Selected = false })
	case "e":
		t.setAll(func(n *export.Node) { n.Expanded = true })
	case "c":
		t.setAll(func(n *export.Node) { n.Expanded = false })
	case "enter":
		if sel, _ := m.tree.Counts(); sel == 0 {
			return m, nil
		}
		m.options = newOptions(m.cfg, m.space)
		m.screen = scrOptions
		cmd := m.options.focusCmd()
		return m, cmd
	case "esc":
		m.screen = scrSpaces
	}
	return m, nil
}

func fullySelected(n *export.Node) bool {
	if !n.Selected {
		return false
	}
	for _, c := range n.Children {
		if !fullySelected(c) {
			return false
		}
	}
	return true
}

func anySelected(n *export.Node) bool {
	if n.Selected {
		return true
	}
	for _, c := range n.Children {
		if anySelected(c) {
			return true
		}
	}
	return false
}

func countDescendants(n *export.Node) int {
	c := len(n.Children)
	for _, ch := range n.Children {
		c += countDescendants(ch)
	}
	return c
}

func (m Model) viewTree() string {
	t := m.treeView
	sel, total := m.tree.Counts()
	var b strings.Builder
	b.WriteString(sHeading.Render("Choose the pages to export") + sMuted.Render("  ·  "+m.space.Name) + "\n")
	countStyle := sSuccess
	if sel == 0 {
		countStyle = sError
	}
	b.WriteString(countStyle.Render(fmt.Sprintf("%d of %d pages selected", sel, total)) +
		sMuted.Render("  ·  ticking a page includes its sub-pages") + "\n\n")

	width := m.width - 6
	end := min(t.offset+t.height, len(t.visible))
	for i := t.offset; i < end; i++ {
		n := t.visible[i]
		indent := strings.Repeat("   ", n.Depth)
		arrow := "  "
		if len(n.Children) > 0 {
			arrow = "▸ "
			if n.Expanded {
				arrow = "▾ "
			}
		}
		box := "[ ]"
		switch {
		case fullySelected(n):
			box = "[" + sSuccess.Render("x") + "]"
		case anySelected(n):
			box = "[" + sWarn.Render("~") + "]"
		}
		title := n.Title
		extra := ""
		if n.IsFolder() {
			extra = " (folder)"
		}
		if !n.Expanded && len(n.Children) > 0 {
			extra += fmt.Sprintf("  %d sub-pages", countDescendants(n))
		}
		room := width - lipgloss.Width(indent+arrow+"[x] ") - len(extra)
		title = truncate(title, room)
		var line string
		if i == t.cursor {
			line = sKey.Render("›") + " " + indent + arrow + box + " " + sCursor.Render(title) + sMuted.Render(extra)
		} else {
			line = "  " + indent + sMuted.Render(arrow) + box + " " + sText.Render(title) + sMuted.Render(extra)
		}
		b.WriteString(line + "\n")
	}
	if len(t.visible) > t.height {
		b.WriteString(sMuted.Render(fmt.Sprintf("  … showing %d–%d of %d (use ↑/↓, PgUp/PgDn)", t.offset+1, end, len(t.visible))))
	}
	return b.String()
}
