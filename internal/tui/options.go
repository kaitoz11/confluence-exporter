package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kaitoz11/confluence-exporter/internal/config"
	"github.com/kaitoz11/confluence-exporter/internal/confluence"
	"github.com/kaitoz11/confluence-exporter/internal/export"
)

const (
	optFolder = iota
	optAttachments
	optWebsite
	optPDF
	optSpeed
	optStart
	optFields
)

type optionsModel struct {
	folder      textinput.Model
	attachments bool
	website     bool
	pdf         bool
	speed       export.Speed
	focus       int
	space       confluence.Space
	err         string
}

func newOptions(cfg config.Config, space confluence.Space) optionsModel {
	in := textinput.New()
	in.SetValue(cfg.OutputDir)
	in.CharLimit = 1024
	in.Width = 60
	in.Prompt = ""
	o := optionsModel{folder: in, attachments: true, website: true, pdf: true, focus: optStart, space: space}
	if cfg.Attachments != nil {
		o.attachments = *cfg.Attachments
	}
	if cfg.Website != nil {
		o.website = *cfg.Website
	}
	if cfg.PDF != nil {
		o.pdf = *cfg.PDF
	}
	o.speed = export.ParseSpeed(cfg.Speed)
	return o
}

func (o *optionsModel) focusCmd() tea.Cmd {
	if o.focus == optFolder {
		return o.folder.Focus()
	}
	o.folder.Blur()
	return nil
}

func (o optionsModel) outputDir() string { return config.ExpandPath(o.folder.Value()) }

func (m Model) updateOptions(msg tea.Msg) (tea.Model, tea.Cmd) {
	o := &m.options
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			m.screen = scrTree
			return m, nil
		case "tab", "down":
			o.focus = (o.focus + 1) % optFields
			cmd := o.focusCmd()
			return m, cmd
		case "left", "right":
			if o.focus == optSpeed {
				d := 1
				if k.String() == "left" {
					d = -1
				}
				o.speed = nextSpeed(o.speed, d)
				return m, nil
			}
		case "shift+tab", "up":
			o.focus = (o.focus + optFields - 1) % optFields
			cmd := o.focusCmd()
			return m, cmd
		case " ", "enter":
			switch o.focus {
			case optAttachments:
				o.attachments = !o.attachments
				return m, nil
			case optWebsite:
				o.website = !o.website
				return m, nil
			case optPDF:
				o.pdf = !o.pdf
				return m, nil
			case optSpeed:
				o.speed = nextSpeed(o.speed, 1)
				return m, nil
			case optStart:
				return m.tryStart()
			case optFolder:
				if k.String() == "enter" {
					o.focus = optStart
					cmd := o.focusCmd()
					return m, cmd
				}
			}
		}
	}
	if o.focus == optFolder {
		var cmd tea.Cmd
		o.folder, cmd = o.folder.Update(msg)
		if _, isKey := msg.(tea.KeyMsg); isKey {
			o.err = ""
		}
		return m, cmd
	}
	return m, nil
}

func (m Model) tryStart() (tea.Model, tea.Cmd) {
	dir := m.options.outputDir()
	if dir == "" {
		m.options.err = "Please choose a folder to save the export in."
		m.options.focus = optFolder
		cmd := m.options.focusCmd()
		return m, cmd
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		m.options.err = "Cannot use this folder: " + err.Error()
		m.options.focus = optFolder
		cmd := m.options.focusCmd()
		return m, cmd
	}
	cmd := m.startExport()
	return m, cmd
}

func (m Model) viewOptions() string {
	o := m.options
	sel, _ := m.tree.Counts()
	var b strings.Builder
	b.WriteString(sHeading.Render("Export options") + "\n")
	b.WriteString(sMuted.Render("Exporting ") + sBold.Render(itoa(sel)+" pages") + sMuted.Render(" from ") + sBold.Render(m.space.Name) + "\n\n")

	b.WriteString(inputRow("Save in folder", o.folder.View(), o.focus == optFolder) + "\n")
	b.WriteString(hintIndent + sMuted.Render("Tip: drag a folder into this window to paste its path") + "\n\n")

	b.WriteString(checkbox("Download attachments (images and files)", o.attachments, o.focus == optAttachments) + "\n")
	b.WriteString(checkbox("Create a website to browse the space offline (index.html)", o.website, o.focus == optWebsite) + "\n")
	b.WriteString(checkbox("Also create a PDF for every page", o.pdf, o.focus == optPDF) + "\n")
	b.WriteString(speedRow(o.speed, o.focus == optSpeed) + "\n\n")

	target := export.SpaceFolder(o.outputDir(), m.space)
	b.WriteString(sMuted.Render("  Files will be saved to:") + "\n    " + sText.Render(target) + "\n")
	if entries, err := os.ReadDir(target); err == nil && len(entries) > 0 {
		b.WriteString("  " + sWarn.Render("This folder already exists: pages will be updated, downloaded attachments are reused.") + "\n")
	}
	b.WriteString("\n" + button("Start export", o.focus == optStart) + "\n")
	if o.err != "" {
		b.WriteString("\n  " + sError.Render("✗ "+o.err) + "\n")
	}
	return b.String()
}

func nextSpeed(s export.Speed, d int) export.Speed {
	n := len(export.Speeds)
	return export.Speeds[(int(s)+d+n)%n]
}

// speedRow renders "Speed  ‹ Gentle ›  description".
func speedRow(s export.Speed, focused bool) string {
	value := "‹ " + s.String() + " ›"
	desc := sMuted.Render("  " + s.Description())
	if focused {
		return sKey.Render("›") + " " + sText.Render("Speed ") + sCursor.Render(value) + desc
	}
	return "  " + sText.Render("Speed ") + sBold.Render(value) + desc
}
