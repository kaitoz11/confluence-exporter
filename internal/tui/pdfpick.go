package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kaitoz11/confluence-exporter/internal/config"
	"github.com/kaitoz11/confluence-exporter/internal/export"
)

type pdfPickModel struct {
	folder     textinput.Model
	speed      export.Speed
	speedFocus bool
	err        string
}

func newPDFPick(cfg config.Config) pdfPickModel {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 1024
	in.Width = 60
	in.Placeholder = "folder containing .md files"
	dir := cfg.LastPDFDir
	if dir == "" {
		dir = cfg.OutputDir
	}
	in.SetValue(dir)
	in.CursorEnd()
	in.Focus()
	return pdfPickModel{folder: in, speed: export.ParseSpeed(cfg.Speed)}
}

func (m Model) updatePDFPick(msg tea.Msg) (tea.Model, tea.Cmd) {
	p := &m.pdfPick
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			m.screen = scrMenu
			return m, nil
		case "tab", "shift+tab", "up", "down":
			p.speedFocus = !p.speedFocus
			if p.speedFocus {
				p.folder.Blur()
				return m, nil
			}
			cmd := p.folder.Focus()
			return m, cmd
		case "left", "right", " ":
			if p.speedFocus {
				d := 1
				if k.String() == "left" {
					d = -1
				}
				p.speed = nextSpeed(p.speed, d)
				return m, nil
			}
		case "enter":
			dir := config.ExpandPath(p.folder.Value())
			fi, err := os.Stat(dir)
			if err != nil || !fi.IsDir() {
				p.err = "This folder does not exist. Check the path, or drag the folder into this window."
				return m, nil
			}
			cmd := m.startPDF(dir, p.speed)
			return m, cmd
		}
	}
	if p.speedFocus {
		return m, nil
	}
	var cmd tea.Cmd
	p.folder, cmd = p.folder.Update(msg)
	if _, isKey := msg.(tea.KeyMsg); isKey {
		p.err = ""
	}
	return m, cmd
}

func (m Model) viewPDFPick() string {
	p := m.pdfPick
	var b strings.Builder
	b.WriteString(sHeading.Render("Convert Markdown files to PDF") + "\n")
	b.WriteString(sMuted.Render("Every .md file in the folder and its sub-folders gets a .pdf next to it,") + "\n")
	b.WriteString(sMuted.Render("with the images from its _attachments folder.") + "\n\n")
	b.WriteString(inputRow("Folder", p.folder.View(), !p.speedFocus) + "\n")
	b.WriteString(hintIndent + sMuted.Render("Tip: drag a folder into this window to paste its path") + "\n\n")
	b.WriteString(speedRow(p.speed, p.speedFocus) + "\n")
	if p.err != "" {
		b.WriteString("\n  " + sError.Render("✗ "+p.err) + "\n")
	}
	return b.String()
}
