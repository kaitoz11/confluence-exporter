package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type menuItem struct {
	title, desc string
}

type menuModel struct {
	items      []menuItem
	cursor     int
	helpScroll int
}

const (
	menuExport = iota
	menuSite
	menuPDF
	menuHelp
	menuQuit
)

func newMenu() menuModel {
	return menuModel{items: []menuItem{
		{"Export a Confluence space", "Save every page as Markdown, with attachments, a browsable website and PDFs"},
		{"Create a website from Markdown files", "Browse an export like Confluence, offline: page tree, search, images and files"},
		{"Convert Markdown files to PDF", "Turn a folder of .md files (for example a previous export) into PDF files"},
		{"Help: getting an API token", "Step-by-step instructions for Confluence Cloud and Server / Data Center"},
		{"Quit", ""},
	}}
}

func (m Model) updateMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "up", "k", "shift+tab":
		m.menu.cursor = (m.menu.cursor + len(m.menu.items) - 1) % len(m.menu.items)
	case "down", "j", "tab":
		m.menu.cursor = (m.menu.cursor + 1) % len(m.menu.items)
	case "1", "2", "3", "4", "5":
		m.menu.cursor = int(k.String()[0] - '1')
		return m.chooseMenu()
	case "enter", " ":
		return m.chooseMenu()
	case "q", "esc":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) chooseMenu() (tea.Model, tea.Cmd) {
	switch m.menu.cursor {
	case menuExport:
		m.screen = scrLogin
		cmd := m.login.focusCmd()
		return m, cmd
	case menuPDF, menuSite:
		mode := pickPDF
		if m.menu.cursor == menuSite {
			mode = pickSite
		}
		m.pdfPick = newPDFPick(m.cfg, mode)
		m.screen = scrPDFPick
		return m, nil
	case menuHelp:
		m.screen = scrHelp
		return m, nil
	}
	return m, tea.Quit
}

func (m Model) viewMenu() string {
	var b strings.Builder
	b.WriteString(sText.Render("Welcome! This tool copies a whole Confluence space to your computer.") + "\n")
	b.WriteString(sMuted.Render("Use the arrow keys to choose, then press Enter.") + "\n\n")
	width := min(m.width-8, 90)
	for i, it := range m.menu.items {
		num := sMuted.Render(string(rune('1'+i)) + ". ")
		if i == m.menu.cursor {
			b.WriteString(sKey.Render("› ") + num + sCursor.Render(" "+it.title+" ") + "\n")
		} else {
			b.WriteString("  " + num + sBold.Render(" "+it.title) + "\n")
		}
		if it.desc != "" {
			b.WriteString("       " + sMuted.Render(truncate(it.desc, width)) + "\n")
		}
		b.WriteString("\n")
	}
	if m.version != "" {
		b.WriteString(sMuted.Render("version " + m.version))
	}
	return b.String()
}

func (m Model) updateHelp(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc", "enter", "q", "backspace":
			m.screen = scrMenu
		case "up", "k":
			m.menu.helpScroll = max(m.menu.helpScroll-1, 0)
		case "down", "j", " ":
			m.menu.helpScroll = min(m.menu.helpScroll+1, max(len(helpLines())-m.bodyHeight(), 0))
		}
	}
	return m, nil
}

func (m Model) viewHelp() string {
	lines := helpLines()
	start := min(m.menu.helpScroll, max(len(lines)-1, 0))
	end := min(start+m.bodyHeight(), len(lines))
	return strings.Join(lines[start:end], "\n")
}

func helpLines() []string {
	h := sHeading.Render
	t := sText.Render
	mu := sMuted.Render
	lines := []string{
		h("Confluence Cloud") + mu("  (address ends with .atlassian.net)"),
		t("  1. Open ") + sKey.Render("https://id.atlassian.com/manage-profile/security/api-tokens"),
		t("  2. Click \"Create API token\", give it a name, and copy the token."),
		t("  3. In this tool, enter your Confluence address, your work email and the token."),
		"",
		h("Confluence Server / Data Center") + mu("  (your company's own Confluence address)"),
		t("  1. In Confluence, click your profile picture → Settings → Personal Access Tokens."),
		t("  2. Click \"Create token\" and copy it."),
		t("  3. In this tool, enter the address and the token. Leave the username empty."),
		mu("     If your server has no personal access tokens, enter your username and password instead."),
		"",
		h("Tips"),
		t("  • You can paste any Confluence link (even a page link); the space is pre-selected for you."),
		t("  • To paste in the terminal use Cmd+V (Mac) or Ctrl+Shift+V / right-click (Windows, Linux)."),
		t("  • Running the same export again updates the files and skips attachments already downloaded."),
		t("  • Computer feels slow? Choose the Gentle speed: it uses less memory and processor."),
		t("  • Open index.html in the export to browse it like Confluence, even without internet."),
		"",
		h("What you get"),
		t("  <Space name>/"),
		t("    index.html              ") + mu("← open this to browse the space in your web browser"),
		t("    Table of Contents.md"),
		t("    Home page/"),
		t("      Home page.md          ") + mu("← the page as Markdown"),
		t("      Home page.html        ") + mu("← page of the website"),
		t("      Home page.pdf         ") + mu("← optional PDF version"),
		t("      _attachments/         ") + mu("← images and files of this page"),
		t("      Child page/           ") + mu("← sub-pages, nested exactly like in Confluence"),
	}
	return lines
}
