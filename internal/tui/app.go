// Package tui is the interactive terminal interface.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kaitoz11/confluence-exporter/internal/config"
	"github.com/kaitoz11/confluence-exporter/internal/confluence"
	"github.com/kaitoz11/confluence-exporter/internal/export"
)

type screen int

const (
	scrMenu screen = iota
	scrHelp
	scrLogin
	scrBusy
	scrSpaces
	scrTree
	scrOptions
	scrRunning
	scrDone
	scrPDFPick
)

// Model is the root Bubble Tea model.
type Model struct {
	version string
	width   int
	height  int
	screen  screen

	cfg    config.Config
	site   confluence.Site
	client confluence.Client
	user   string
	space  confluence.Space
	tree   *export.Tree

	menu      menuModel
	login     loginModel
	spinner   spinner.Model
	busyMsg   string
	busyBack  screen
	spaceList list.Model
	treeView  treeModel
	options   optionsModel
	run       runModel
	done      doneModel
	pdfPick   pdfPickModel

	// cancel stops the running background operation, if any.
	cancel context.CancelFunc
	events chan tea.Msg
}

// New creates the app.
func New(version string) Model {
	cfg := config.Load()
	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(sKey))
	return Model{
		version: version,
		cfg:     cfg,
		spinner: sp,
		menu:    newMenu(),
		login:   newLogin(cfg),
		width:   80,
		height:  24,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tea.SetWindowTitle("Confluence Space Exporter"), m.spinner.Tick)
}

// listen waits for the next message from a background operation.
func listen(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// startOp runs fn in the background with a cancelable context. fn sends
// progress messages with send and returns its final message.
func (m *Model) startOp(fn func(ctx context.Context, send func(tea.Msg)) tea.Msg) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	ch := make(chan tea.Msg, 256)
	m.events = ch
	go func() {
		defer close(ch)
		ch <- fn(ctx, func(msg tea.Msg) { ch <- msg })
	}()
	return listen(ch)
}

func (m *Model) stopOp() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.screen == scrRunning && !m.run.confirmQuit {
				m.run.confirmQuit = true
				return m, nil
			}
			m.stopOp()
			return m, tea.Quit
		}
	}

	// Messages from background work: keep listening on the channel.
	if isOpMsg(msg) {
		model, cmd := m.handleOp(msg)
		mm := model.(Model)
		if mm.events != nil {
			cmd = tea.Batch(cmd, listen(mm.events))
		}
		return mm, cmd
	}

	switch m.screen {
	case scrMenu:
		return m.updateMenu(msg)
	case scrHelp:
		return m.updateHelp(msg)
	case scrLogin:
		return m.updateLogin(msg)
	case scrBusy:
		if k, ok := msg.(tea.KeyMsg); ok && k.String() == "esc" {
			m.stopOp()
			m.screen = m.busyBack
		}
		return m, nil
	case scrSpaces:
		return m.updateSpaces(msg)
	case scrTree:
		return m.updateTree(msg)
	case scrOptions:
		return m.updateOptions(msg)
	case scrRunning:
		return m.updateRunning(msg)
	case scrDone:
		return m.updateDone(msg)
	case scrPDFPick:
		return m.updatePDFPick(msg)
	}
	return m, nil
}

func (m *Model) busy(text string, back screen) {
	m.busyMsg = text
	m.busyBack = back
	m.screen = scrBusy
}

func (m *Model) resize() {
	bodyH := m.bodyHeight()
	if m.spaceList.Items() != nil {
		m.spaceList.SetSize(m.width-4, bodyH)
	}
	m.treeView.setHeight(bodyH - 4)
}

// bodyHeight is the room left between header and footer.
func (m Model) bodyHeight() int {
	h := m.height - 6
	if h < 5 {
		h = 5
	}
	return h
}

var steps = []string{"Connect", "Space", "Pages", "Options", "Export"}

func (m Model) stepIndex() int {
	switch m.screen {
	case scrLogin:
		return 0
	case scrSpaces:
		return 1
	case scrTree:
		return 2
	case scrOptions:
		return 3
	case scrRunning, scrDone:
		if m.run.pdfOnly {
			return -1
		}
		return 4
	case scrBusy:
		return m.busyStep()
	}
	return -1
}

func (m Model) busyStep() int {
	switch m.busyBack {
	case scrLogin:
		return 0
	case scrSpaces:
		return 1
	}
	return -1
}

func (m Model) header() string {
	title := sTitle.Render("Confluence Space Exporter")
	right := ""
	if m.user != "" {
		right = sMuted.Render("Signed in as ") + sBold.Render(m.user)
	}
	line := title
	if gap := m.width - lipgloss.Width(title) - lipgloss.Width(right) - 2; gap > 0 && right != "" {
		line += strings.Repeat(" ", gap) + right
	}
	cur := m.stepIndex()
	if cur < 0 {
		return line
	}
	var crumbs []string
	for i, s := range steps {
		label := fmt.Sprintf("%d %s", i+1, s)
		switch {
		case i == cur:
			crumbs = append(crumbs, sHeading.Underline(true).Render(label))
		case i < cur:
			crumbs = append(crumbs, sSuccess.Render("✓ "+s))
		default:
			crumbs = append(crumbs, sMuted.Render(label))
		}
	}
	return line + "\n" + strings.Join(crumbs, sMuted.Render("  ›  "))
}

func (m Model) footer() string {
	switch m.screen {
	case scrMenu:
		return keyHelp("↑/↓", "move", "enter", "choose", "q", "quit")
	case scrHelp:
		return keyHelp("↑/↓", "scroll", "esc", "back")
	case scrLogin:
		return keyHelp("tab/↓", "next", "↑", "previous", "enter", "continue", "ctrl+r", "show token", "esc", "menu")
	case scrBusy:
		return keyHelp("esc", "cancel")
	case scrSpaces:
		return keyHelp("↑/↓", "move", "/", "search", "enter", "choose", "esc", "back")
	case scrTree:
		return keyHelp("space", "tick", "→/←", "open/close", "a/n", "all/none", "enter", "continue", "esc", "back")
	case scrOptions:
		return keyHelp("tab/↓", "next", "space", "tick", "←/→", "speed", "enter", "start", "esc", "back")
	case scrRunning:
		if m.run.throttle != nil {
			return keyHelp("←/→", "change speed", "esc", "stop")
		}
		return keyHelp("esc", "stop")
	case scrDone:
		return m.done.footer()
	case scrPDFPick:
		if m.pdfPick.mode == pickSite {
			return keyHelp("enter", "create website", "esc", "back")
		}
		return keyHelp("tab", "folder/speed", "←/→", "speed", "enter", "convert", "esc", "back")
	}
	return ""
}

func (m Model) View() string {
	var body string
	switch m.screen {
	case scrMenu:
		body = m.viewMenu()
	case scrHelp:
		body = m.viewHelp()
	case scrLogin:
		body = m.viewLogin()
	case scrBusy:
		body = "\n  " + m.spinner.View() + " " + sText.Render(m.busyMsg) + "\n\n  " + sMuted.Render("This can take a moment for large spaces.")
	case scrSpaces:
		body = m.spaceList.View()
	case scrTree:
		body = m.viewTree()
	case scrOptions:
		body = m.viewOptions()
	case scrRunning:
		body = m.viewRunning()
	case scrDone:
		body = m.viewDone()
	case scrPDFPick:
		body = m.viewPDFPick()
	}
	header := m.header()
	footer := m.footer()
	// Pin the footer to the bottom of the window.
	used := lipgloss.Height(header) + lipgloss.Height(footer) + 2
	body = lipgloss.NewStyle().Height(max(m.height-used, 1)).MaxHeight(max(m.height-used, 1)).Render(body)
	return lipgloss.NewStyle().Padding(0, 1).Render(header + "\n\n" + body + "\n" + footer)
}
