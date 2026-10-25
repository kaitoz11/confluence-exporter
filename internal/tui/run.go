package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kaitoz11/confluence-exporter/internal/export"
)

const logLines = 10

type logLine struct {
	text string
	warn bool
}

// runModel shows the progress of an export or a PDF conversion.
type runModel struct {
	title       string
	target      string
	pdfOnly     bool
	phase       string
	done, total int
	warnings    int
	log         []logLine
	started     time.Time
	phaseStart  time.Time
	bar         progress.Model
	confirmQuit bool
	stopping    bool
}

func newRunModel(title string, pdfOnly bool) runModel {
	return runModel{
		title:      title,
		pdfOnly:    pdfOnly,
		started:    time.Now(),
		phaseStart: time.Now(),
		bar:        progress.New(progress.WithDefaultGradient(), progress.WithoutPercentage()),
		phase:      "Starting…",
	}
}

func (r *runModel) apply(e export.Event) {
	if e.Phase != "" {
		if e.Phase != r.phase {
			r.done = 0
			r.phaseStart = time.Now()
		}
		r.phase = e.Phase
		r.total = e.Total
		r.done = max(r.done, e.Done)
	}
	if e.Message != "" {
		r.push(logLine{text: e.Message})
	}
	if e.Warning != "" {
		r.warnings++
		r.push(logLine{text: "⚠ " + e.Warning, warn: true})
	}
}

func (r *runModel) push(l logLine) {
	r.log = append(r.log, l)
	if len(r.log) > logLines {
		r.log = r.log[len(r.log)-logLines:]
	}
}

func (m Model) updateRunning(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.run.confirmQuit {
		switch k.String() {
		case "y", "Y", "enter":
			m.run.confirmQuit = false
			m.run.stopping = true
			m.stopOp() // the operation finishes and reports what was saved
		default:
			m.run.confirmQuit = false
		}
		return m, nil
	}
	if k.String() == "esc" || k.String() == "q" {
		m.run.confirmQuit = true
	}
	return m, nil
}

func (m Model) viewRunning() string {
	r := m.run
	var b strings.Builder
	b.WriteString(sHeading.Render(r.title) + sMuted.Render("  ·  "+r.target) + "\n\n")

	pct := 0.0
	if r.total > 0 {
		pct = float64(r.done) / float64(r.total)
	}
	r.bar.Width = max(min(m.width-30, 70), 10)
	b.WriteString("  " + m.spinner.View() + " " + sBold.Render(r.phase) + "\n")
	b.WriteString("  " + r.bar.ViewAs(pct) + "  " + sText.Render(fmt.Sprintf("%d / %d  (%d%%)", r.done, r.total, int(pct*100))) + "\n\n")

	elapsed := time.Since(r.started).Round(time.Second)
	stats := sMuted.Render("  Elapsed ") + sText.Render(elapsed.String())
	if eta := estimate(r); eta != "" {
		stats += sMuted.Render("  ·  about ") + sText.Render(eta) + sMuted.Render(" left")
	}
	if r.warnings > 0 {
		stats += sMuted.Render("  ·  ") + sWarn.Render(strconv.Itoa(r.warnings)+" warnings")
	}
	b.WriteString(stats + "\n\n")

	b.WriteString(sMuted.Render("  Recent activity") + "\n")
	width := m.width - 8
	// Show as much recent activity as the window allows.
	shown := r.log[max(len(r.log)-max(m.bodyHeight()-10, 2), 0):]
	for _, l := range shown {
		text := truncate(l.text, width)
		if l.warn {
			b.WriteString("    " + sWarn.Render(text) + "\n")
		} else {
			b.WriteString("    " + sText.Render(text) + "\n")
		}
	}
	switch {
	case r.stopping:
		b.WriteString("\n  " + sWarn.Render("Stopping… finishing the files in progress."))
	case r.confirmQuit:
		b.WriteString("\n  " + sWarn.Bold(true).Render("Stop now? Pages saved so far are kept.  (y = stop, any other key = continue)"))
	}
	return b.String()
}

// estimate extrapolates the time left from the pace of the current phase.
func estimate(r runModel) string {
	if r.done < 3 || r.total == 0 || r.done >= r.total {
		return ""
	}
	per := time.Since(r.phaseStart) / time.Duration(r.done)
	left := (per * time.Duration(r.total-r.done)).Round(time.Second)
	if left < 5*time.Second {
		return ""
	}
	return left.String()
}

func itoa(i int) string { return strconv.Itoa(i) }
