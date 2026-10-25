package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kaitoz11/confluence-exporter/internal/config"
	"github.com/kaitoz11/confluence-exporter/internal/confluence"
	"github.com/kaitoz11/confluence-exporter/internal/export"
)

// Messages produced by background operations.
type (
	connectedMsg struct {
		user   string
		spaces []confluence.Space
		err    error
	}
	pageCountMsg  int
	treeLoadedMsg struct {
		pages []confluence.Page
		err   error
	}
	exportEventMsg export.Event
	exportDoneMsg  export.Result
)

func isOpMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case connectedMsg, pageCountMsg, treeLoadedMsg, exportEventMsg, exportDoneMsg:
		return true
	}
	return false
}

func (m Model) handleOp(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case connectedMsg:
		m.events, m.cancel = nil, nil
		if m.screen != scrBusy {
			return m, nil // canceled
		}
		if msg.err != nil {
			m.screen = scrLogin
			m.login.err = msg.err.Error()
			return m, nil
		}
		m.user = msg.user
		m.saveLogin()
		if len(msg.spaces) == 0 {
			m.screen = scrLogin
			m.login.err = "You are signed in, but your account cannot see any spaces."
			return m, nil
		}
		m.spaceList = newSpaceList(msg.spaces, m.width-4, m.bodyHeight())
		m.selectSpaceKey(m.site.SpaceKey)
		m.screen = scrSpaces
		return m, nil

	case pageCountMsg:
		m.busyMsg = fmt.Sprintf("Reading the page tree of %s… %d pages found", m.space.Name, int(msg))
		return m, nil

	case treeLoadedMsg:
		m.events, m.cancel = nil, nil
		if m.screen != scrBusy {
			return m, nil
		}
		if msg.err != nil {
			m.screen = scrSpaces
			m.spaceList.NewStatusMessage(sError.Render("Could not read the space: " + msg.err.Error()))
			return m, nil
		}
		if len(msg.pages) == 0 {
			m.screen = scrSpaces
			m.spaceList.NewStatusMessage(sWarn.Render("This space has no pages you can read."))
			return m, nil
		}
		m.tree = export.BuildTree(m.space, msg.pages)
		m.treeView = newTreeModel(m.tree, m.bodyHeight()-4)
		m.screen = scrTree
		return m, nil

	case exportEventMsg:
		m.run.apply(export.Event(msg))
		return m, nil

	case exportDoneMsg:
		m.events, m.cancel = nil, nil
		m.done = newDoneModel(export.Result(msg), m.run)
		m.screen = scrDone
		return m, nil
	}
	return m, nil
}

func (m *Model) connect() tea.Cmd {
	site, err := confluence.ParseSiteURL(m.login.inputs[fieldURL].Value())
	if err != nil {
		m.login.err = err.Error()
		return nil
	}
	creds := confluence.Credentials{
		URL:   site.Base,
		Email: m.login.inputs[fieldEmail].Value(),
		Token: m.login.inputs[fieldToken].Value(),
	}
	m.site = site
	m.client = confluence.New(site, creds)
	m.login.err = ""
	m.busy("Connecting to "+site.Base+"…", scrLogin)
	client := m.client
	return m.startOp(func(ctx context.Context, _ func(tea.Msg)) tea.Msg {
		user, err := client.CurrentUser(ctx)
		if err != nil {
			return connectedMsg{err: err}
		}
		spaces, err := client.ListSpaces(ctx)
		return connectedMsg{user: user, spaces: spaces, err: err}
	})
}

func (m *Model) saveLogin() {
	m.cfg.URL = m.login.inputs[fieldURL].Value()
	m.cfg.Email = m.login.inputs[fieldEmail].Value()
	m.cfg.Token = ""
	if m.login.remember {
		m.cfg.Token = m.login.inputs[fieldToken].Value()
	}
	_ = config.Save(m.cfg)
}

func (m *Model) loadTree(space confluence.Space) tea.Cmd {
	m.space = space
	m.busy("Reading the page tree of "+space.Name+"…", scrSpaces)
	client := m.client
	return m.startOp(func(ctx context.Context, send func(tea.Msg)) tea.Msg {
		pages, err := client.ListPages(ctx, space, func(n int) { send(pageCountMsg(n)) })
		return treeLoadedMsg{pages: pages, err: err}
	})
}

func (m *Model) startExport() tea.Cmd {
	opts := export.Options{
		OutputDir:   m.options.outputDir(),
		Attachments: m.options.attachments,
		PDF:         m.options.pdf,
		Speed:       m.options.speed,
	}
	m.cfg.OutputDir = opts.OutputDir
	m.cfg.Attachments = &opts.Attachments
	m.cfg.PDF = &opts.PDF
	m.cfg.Speed = opts.Speed.String()
	_ = config.Save(m.cfg)

	sel, _ := m.tree.Counts()
	m.run = newRunModel("Exporting "+m.space.Name, false)
	m.run.target = fmt.Sprintf("%d pages", sel)
	m.screen = scrRunning
	client, tree := m.client, m.tree
	return m.startOp(func(ctx context.Context, send func(tea.Msg)) tea.Msg {
		res := export.Run(ctx, client, tree, opts, func(e export.Event) { send(exportEventMsg(e)) })
		return exportDoneMsg(res)
	})
}

func (m *Model) startPDF(dir string, speed export.Speed) tea.Cmd {
	m.cfg.LastPDFDir = dir
	m.cfg.Speed = speed.String()
	_ = config.Save(m.cfg)
	m.run = newRunModel("Converting Markdown to PDF", true)
	m.run.target = dir
	m.screen = scrRunning
	return m.startOp(func(ctx context.Context, send func(tea.Msg)) tea.Msg {
		res := export.ConvertFolderToPDF(ctx, dir, speed, func(e export.Event) { send(exportEventMsg(e)) })
		return exportDoneMsg(res)
	})
}
