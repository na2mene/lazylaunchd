package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/na2mene/lazylaunchd/internal/launchd"
)

// The Tools screen makes the CLI subcommands (export / import / doctor /
// setup) discoverable from inside the TUI — no need to remember them.

var toolsItems = []string{
	"Export — all user agents to a JSON file",
	"Import — jobs from a JSON file…",
	"Doctor — health check report",
	"Watcher — install / update & restart",
}

func (m Model) openTools() (Model, tea.Cmd) {
	m.mode = toolsView
	m.toolsCursor = 0
	m.status = ""
	return m, nil
}

func (m Model) updateTools(key string) (Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.mode = listView
	case "j", "down":
		if m.toolsCursor < len(toolsItems)-1 {
			m.toolsCursor++
		}
	case "k", "up":
		if m.toolsCursor > 0 {
			m.toolsCursor--
		}
	case "enter":
		switch m.toolsCursor {
		case 0:
			m.mode = toolExportView
			home, _ := os.UserHomeDir()
			def := filepath.Join(home, "Desktop", "lazylaunchd-jobs-"+time.Now().Format("20060102")+".json")
			m.toolInput = newToolInput(shortenHome(def))
			m.toolCands = listCandidates(m.toolInput.Value())
			return m, textinput.Blink
		case 1:
			m.mode = toolImportView
			m.toolInput = newToolInput("~/Desktop/")
			m.toolCands = listCandidates(m.toolInput.Value())
			return m, textinput.Blink
		case 2:
			report, _ := launchd.Doctor(Version)
			return m.showReport("Doctor", report), nil
		case 3:
			msg, err := launchd.SetupWatcher()
			if err != nil {
				msg = "setup failed: " + err.Error()
			}
			m = m.rescan()
			return m.showReport("Watcher setup", msg), nil
		}
	}
	return m, nil
}

func newToolInput(value string) textinput.Model {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 250
	ti.Width = 60
	ti.SetValue(value)
	ti.CursorEnd()
	return ti
}

func (m Model) updateToolExport(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.mode = toolsView
			return m, nil
		case "tab":
			val, _ := completePath(m.toolInput.Value())
			m.toolInput.SetValue(val)
			m.toolInput.CursorEnd()
			m.toolCands = listCandidates(val)
			return m, nil
		case "enter":
			raw := strings.TrimSpace(m.toolInput.Value())
			if raw == "" {
				return m, nil
			}
			path := expandTilde(raw)
			if _, err := os.Stat(path); err == nil {
				return m.showReport("Export", "already exists: "+shortenHome(path)+"\nchoose another name (esc → Export to retry)"), nil
			}
			data, count, err := launchd.ExportUserAgents()
			if err != nil {
				return m.showReport("Export", "export failed: "+err.Error()), nil
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return m.showReport("Export", "write failed: "+err.Error()), nil
			}
			return m.showReport("Export", fmt.Sprintf(
				"%d job(s) exported to:\n  %s\n\nScripts are not bundled — keep them in a git repo.\nOn another Mac: clone the scripts, then run\n  lazylaunchd import %s",
				count, shortenHome(path), filepath.Base(path))), nil
		}
	}
	var cmd tea.Cmd
	m.toolInput, cmd = m.toolInput.Update(msg)
	m.toolCands = listCandidates(m.toolInput.Value())
	return m, cmd
}

func (m Model) toolExportPanel() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Export jobs") + "\n\n")
	b.WriteString(sectionStyle.Render("Save all user agents as portable JSON to:") + "\n")
	b.WriteString(dimStyle.Render("Nothing is written until you press enter.") + "\n\n")
	b.WriteString("  " + m.toolInput.View() + "\n")
	b.WriteString("\n" + dimStyle.Render("  in: ") + pathLocStyle.Render(completionDir(m.toolInput.Value())) +
		dimStyle.Render(fmt.Sprintf(" · %d candidates", len(m.toolCands))) + "\n")
	if len(m.toolCands) > 0 {
		b.WriteString(renderCandidates(m.toolCands, m.width))
	}
	b.WriteString("\n" + helpStyle.Render("tab complete path · enter write file · esc back"))
	return b.String()
}

func (m Model) showReport(title, body string) Model {
	m.mode = toolReportView
	m.toolTitle = title
	m.toolReport = body
	m.toolScroll = 0
	return m
}

func (m Model) updateToolImport(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.mode = toolsView
			return m, nil
		case "tab":
			val, _ := completePath(m.toolInput.Value())
			m.toolInput.SetValue(val)
			m.toolInput.CursorEnd()
			m.toolCands = listCandidates(val)
			return m, nil
		case "enter":
			raw := strings.TrimSpace(m.toolInput.Value())
			if raw == "" {
				return m, nil
			}
			data, err := os.ReadFile(expandTilde(raw))
			if err != nil {
				return m.showReport("Import", "read failed: "+err.Error()), nil
			}
			summary, err := launchd.ImportJobs(data, false)
			if err != nil {
				return m.showReport("Import", "import failed: "+err.Error()), nil
			}
			m = m.rescan()
			return m.showReport("Import", summary), nil
		}
	}
	var cmd tea.Cmd
	m.toolInput, cmd = m.toolInput.Update(msg)
	m.toolCands = listCandidates(m.toolInput.Value())
	return m, cmd
}

func (m Model) updateToolReport(key string) (Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.mode = toolsView
	case "j", "down":
		lines := strings.Count(m.toolReport, "\n") + 1
		if m.toolScroll < max(0, lines-5) {
			m.toolScroll++
		}
	case "k", "up":
		if m.toolScroll > 0 {
			m.toolScroll--
		}
	}
	return m, nil
}

func (m Model) toolsPanel() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Tools") + "\n\n")
	for i, it := range toolsItems {
		if i == m.toolsCursor {
			b.WriteString(cursorStyle.Render("▸ "+it) + "\n")
		} else {
			b.WriteString("  " + it + "\n")
		}
	}
	b.WriteString("\n" + m.statusLine())
	b.WriteString(helpStyle.Render("↑↓ move · enter select · esc back"))
	return b.String()
}

func (m Model) toolImportPanel() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Import jobs") + "\n\n")
	b.WriteString(sectionStyle.Render("Path to a lazylaunchd export (jobs.json)") + "\n")
	b.WriteString(dimStyle.Render("Jobs are written unloaded and never overwrite existing labels.") + "\n\n")
	b.WriteString("  " + m.toolInput.View() + "\n")
	b.WriteString("\n" + dimStyle.Render("  in: ") + pathLocStyle.Render(completionDir(m.toolInput.Value())) +
		dimStyle.Render(fmt.Sprintf(" · %d candidates", len(m.toolCands))) + "\n")
	if len(m.toolCands) > 0 {
		b.WriteString(renderCandidates(m.toolCands, m.width))
	}
	b.WriteString("\n" + helpStyle.Render("tab complete path · enter import · esc back"))
	return b.String()
}

func (m Model) toolReportPanel() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.toolTitle) + "\n\n")
	lines := strings.Split(strings.TrimRight(m.toolReport, "\n"), "\n")
	avail := max(3, m.height-5)
	from := clamp(m.toolScroll, 0, max(0, len(lines)-avail))
	to := min(len(lines), from+avail)
	for _, l := range lines[from:to] {
		b.WriteString(trunc(l, max(20, m.width-2)) + "\n")
	}
	b.WriteString("\n" + helpStyle.Render("↑↓ scroll · esc back"))
	return b.String()
}
