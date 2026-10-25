package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kaitoz11/confluence-exporter/internal/export"
)

const warningsFile = "export-warnings.txt"

type doneModel struct {
	res        export.Result
	pdfOnly    bool
	scroll     int
	status     string
	reportPath string
}

func newDoneModel(res export.Result, run runModel) doneModel {
	d := doneModel{res: res, pdfOnly: run.pdfOnly}
	// Keep the full list of problems next to the export, so it can be sent
	// to IT support.
	p := filepath.Join(res.Root, warningsFile)
	if len(res.Warnings) == 0 && !res.Canceled {
		os.Remove(p) // from an earlier run
	}
	if len(res.Warnings) > 0 && res.Root != "" {
		body := fmt.Sprintf("%s — %d warnings\n\n%s\n", time.Now().Format(time.RFC1123), len(res.Warnings), strings.Join(res.Warnings, "\n"))
		if os.WriteFile(p, []byte(body), 0o644) == nil {
			d.reportPath = p
		}
	}
	return d
}

func (d doneModel) canMakePDFs() bool {
	return !d.pdfOnly && d.res.Pages > 0 && d.res.PDFs == 0 && !d.res.Canceled
}

// canMakeSite is true after an export that did not build the website.
func (d doneModel) canMakeSite() bool {
	return !d.pdfOnly && d.res.Pages > 0 && d.res.Website == "" && !d.res.Canceled
}

func (d doneModel) footer() string {
	var pairs []string
	switch {
	case d.res.Website != "":
		pairs = append(pairs, "w", "open website")
	case d.canMakeSite():
		pairs = append(pairs, "w", "create website")
	}
	pairs = append(pairs, "o", "open folder")
	if d.canMakePDFs() {
		pairs = append(pairs, "p", "create PDFs now")
	}
	if len(d.res.Warnings) > 0 {
		pairs = append(pairs, "↑/↓", "scroll warnings")
	}
	pairs = append(pairs, "m", "main menu", "q", "quit")
	return keyHelp(pairs...)
}

func (m Model) updateDone(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	d := &m.done
	switch k.String() {
	case "w":
		switch {
		case d.res.Website != "":
			if err := openPath(d.res.Website); err != nil {
				d.status = sError.Render("Could not open the website: " + err.Error())
			} else {
				d.status = sSuccess.Render("Opened the website in your web browser.")
			}
		case d.canMakeSite():
			cmd := m.startSite(d.res.Root)
			return m, cmd
		}
	case "o", "enter":
		if err := openPath(d.res.Root); err != nil {
			d.status = sError.Render("Could not open the folder: " + err.Error())
		} else {
			d.status = sSuccess.Render("Opened the folder in your file manager.")
		}
	case "p":
		if d.canMakePDFs() {
			cmd := m.startPDF(d.res.Root, export.ParseSpeed(m.cfg.Speed))
			return m, cmd
		}
	case "up", "k":
		d.scroll = max(d.scroll-1, 0)
	case "down", "j":
		d.scroll = min(d.scroll+1, max(len(d.res.Warnings)-1, 0))
	case "m", "esc":
		m.screen = scrMenu
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) viewDone() string {
	d := m.done
	r := d.res
	var b strings.Builder
	switch {
	case r.Canceled:
		b.WriteString(sWarn.Bold(true).Render("■ Stopped") + sMuted.Render("  — everything saved before stopping is kept.") + "\n\n")
	case len(r.Warnings) > 0:
		b.WriteString(sSuccess.Bold(true).Render("✓ Finished") + sWarn.Render(fmt.Sprintf("  with %d warnings (see below)", len(r.Warnings))) + "\n\n")
	default:
		b.WriteString(sSuccess.Bold(true).Render("✓ All done!") + "\n\n")
	}

	row := func(label, value string) {
		b.WriteString("  " + sLabel.Width(16).Render(label) + sText.Render(value) + "\n")
	}
	row("Saved in", r.Root)
	if !d.pdfOnly {
		row("Pages", itoa(r.Pages))
		if r.Attachments > 0 {
			row("Attachments", fmt.Sprintf("%d files (%s)", r.Attachments, export.HumanBytes(r.AttachmentBytes)))
		}
	}
	if r.PDFs > 0 || (d.pdfOnly && r.Website == "") {
		row("PDF files", itoa(r.PDFs))
	}
	if r.Website != "" {
		row("Website", filepath.Base(r.Website)+"  "+sMuted.Render("(press w to open it in your browser)"))
	}
	row("Time", r.Duration.Round(time.Second).String())

	switch {
	case r.Website != "":
		b.WriteString("\n  " + sMuted.Render("Press ") + sKey.Render("w") + sMuted.Render(" to browse the space like in Confluence: page tree, search, images and files.") + "\n")
	case !d.pdfOnly && r.Pages > 0:
		b.WriteString("\n  " + sMuted.Render("Start with ") + sText.Render(export.TOCFile) + sMuted.Render(" — it links to every exported page.") + "\n")
	}
	if d.canMakeSite() {
		b.WriteString("  " + sMuted.Render("Want to browse it like a website? Press ") + sKey.Render("w") + sMuted.Render(" to create one.") + "\n")
	}
	if d.canMakePDFs() {
		b.WriteString("  " + sMuted.Render("Want PDFs too? Press ") + sKey.Render("p") + sMuted.Render(" to convert all pages now.") + "\n")
	}

	if len(r.Warnings) > 0 {
		b.WriteString("\n" + sWarn.Render(fmt.Sprintf("  Warnings (%d)", len(r.Warnings))))
		if d.reportPath != "" {
			b.WriteString(sMuted.Render("  — full list saved to " + warningsFile))
		}
		b.WriteString("\n")
		room := max(m.bodyHeight()-lipgloss.Height(b.String())-2, 3)
		end := min(d.scroll+room, len(r.Warnings))
		for _, w := range r.Warnings[d.scroll:end] {
			b.WriteString("    " + sWarn.Render(truncate("• "+w, m.width-8)) + "\n")
		}
	}
	if d.status != "" {
		b.WriteString("\n  " + d.status + "\n")
	}
	return b.String()
}

// openPath opens a folder in Finder, Explorer or the Linux file manager, or
// a file (such as index.html) in its default app.
func openPath(dir string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	return cmd.Start()
}
