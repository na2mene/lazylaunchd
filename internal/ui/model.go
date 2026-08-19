package ui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/na2mene/lazylaunchd/internal/launchd"
	"github.com/na2mene/lazylaunchd/internal/power"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	cursorStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	runningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	confirmStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("124"))
)

type viewMode int

const (
	listView viewMode = iota
	detailView
)

type confirmState struct {
	prompt string
	done   string
	run    func() error
}

type Model struct {
	jobs    []launchd.Job
	power   power.Status
	cursor  int
	mode    viewMode
	log     []string
	logSrc  string
	status  string
	confirm *confirmState
	width   int
	height  int
}

func New(jobs []launchd.Job, pw power.Status) Model {
	return Model{jobs: jobs, power: pw}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if m.confirm != nil {
			switch msg.String() {
			case "y", "Y":
				c := m.confirm
				m.confirm = nil
				if err := c.run(); err != nil {
					m.status = errStyle.Render(err.Error())
				} else {
					m.status = okStyle.Render(c.done)
				}
				m = m.rescan()
			case "ctrl+c":
				return m, tea.Quit
			default:
				m.confirm = nil
				m.status = dimStyle.Render("canceled")
			}
			return m, nil
		}
		m.status = ""
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.mode == detailView {
				m.mode = listView
				return m, nil
			}
			return m, tea.Quit
		case "esc":
			m.mode = listView
		case "j", "down":
			if m.mode == listView && m.cursor < len(m.jobs)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.mode == listView && m.cursor > 0 {
				m.cursor--
			}
		case "g":
			m.cursor = 0
		case "G":
			m.cursor = max(0, len(m.jobs)-1)
		case "enter":
			if m.mode == listView && len(m.jobs) > 0 {
				m.mode = detailView
				m.log, m.logSrc = tailLogFor(m.jobs[m.cursor])
			}
		case "x":
			if len(m.jobs) > 0 {
				j := m.jobs[m.cursor]
				if err := launchd.RunNow(j); err != nil {
					m.status = errStyle.Render(err.Error())
				} else {
					m.status = okStyle.Render("started: " + j.Label)
				}
				m = m.rescan()
			}
		case "u":
			if len(m.jobs) > 0 {
				j := m.jobs[m.cursor]
				switch {
				case j.Kind == launchd.Daemon:
					m.status = errStyle.Render("system daemons need root — use sudo launchctl")
				case j.Loaded:
					m.confirm = &confirmState{
						prompt: fmt.Sprintf("unload %s? this stops the job until you load it again (y/N)", j.Label),
						done:   "unloaded: " + j.Label,
						run:    func() error { return launchd.Unload(j) },
					}
				default:
					if err := launchd.Load(j); err != nil {
						m.status = errStyle.Render(err.Error())
					} else {
						m.status = okStyle.Render("loaded: " + j.Label)
					}
					m = m.rescan()
				}
			}
		case "r":
			m = m.rescan()
		}
	}
	return m, nil
}

// rescan refreshes jobs, power state, and (in detail view) the log tail.
func (m Model) rescan() Model {
	if jobs, err := launchd.Scan(); err == nil {
		m.jobs = jobs
	}
	m.power = power.Read()
	if m.cursor >= len(m.jobs) {
		m.cursor = max(0, len(m.jobs)-1)
	}
	if m.mode == detailView && len(m.jobs) > 0 {
		m.log, m.logSrc = tailLogFor(m.jobs[m.cursor])
	}
	return m
}

func (m Model) View() string {
	if m.width == 0 {
		// No WindowSizeMsg yet (or a pty that reports no size): use a sane default.
		m.width, m.height = 80, 24
	}
	if m.mode == detailView {
		return m.detail()
	}
	return m.list()
}

type row struct {
	header string
	job    int // index into m.jobs, -1 for headers
}

func (m Model) list() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("lazylaunchd") + "  " + dimStyle.Render(m.power.Headline()) + "\n\n")

	rows := []row{}
	lastKind := launchd.Kind(-1)
	counts := map[launchd.Kind]int{}
	for _, j := range m.jobs {
		counts[j.Kind]++
	}
	for i, j := range m.jobs {
		if j.Kind != lastKind {
			rows = append(rows, row{header: fmt.Sprintf("%s (%d)", j.Kind, counts[j.Kind]), job: -1})
			lastKind = j.Kind
		}
		rows = append(rows, row{job: i})
	}

	// Simple scrolling: keep the cursor row visible.
	avail := m.height - 6
	if avail < 3 {
		avail = 3
	}
	cursorRow := 0
	for ri, r := range rows {
		if r.job == m.cursor {
			cursorRow = ri
		}
	}
	start := 0
	if len(rows) > avail && cursorRow >= avail-1 {
		start = cursorRow - avail + 2
	}
	end := min(len(rows), start+avail)

	labelW := min(44, max(24, m.width/3))
	schedW := min(32, max(18, m.width/4))

	for _, r := range rows[start:end] {
		if r.job == -1 {
			b.WriteString(sectionStyle.Render(r.header) + "\n")
			continue
		}
		j := m.jobs[r.job]
		line := fmt.Sprintf("  %s %s  %s  %s",
			stateDot(j),
			pad(trunc(j.Label, labelW), labelW),
			pad(trunc(j.Schedule, schedW), schedW),
			stateText(j),
		)
		if r.job == m.cursor {
			line = cursorStyle.Render("▸" + line[1:])
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n" + m.statusLine())
	b.WriteString(helpStyle.Render("j/k move · enter detail · x run now · u load/unload · r refresh · q quit"))
	return b.String()
}

// statusLine renders the pending confirmation or the last action result.
func (m Model) statusLine() string {
	if m.confirm != nil {
		return confirmStyle.Render(" "+m.confirm.prompt+" ") + "\n"
	}
	if m.status != "" {
		return m.status + "\n"
	}
	return ""
}

func (m Model) detail() string {
	j := m.jobs[m.cursor]
	var b strings.Builder

	b.WriteString(titleStyle.Render(j.Label) + "\n")
	b.WriteString(dimStyle.Render(j.Kind.String()) + "  " + stateText(j) + "\n\n")

	field := func(name, val string) {
		if val == "" {
			val = dimStyle.Render("—")
		}
		b.WriteString(fmt.Sprintf("%s %s\n", sectionStyle.Render(pad(name+":", 10)), val))
	}
	field("Plist", shortenHome(j.PlistPath))
	field("Program", trunc(strings.Join(j.Program, " "), m.width-14))
	field("Schedule", j.Schedule)
	field("Stdout", shortenHome(j.StdoutPath))
	field("Stderr", shortenHome(j.StderrPath))
	if j.ParseError != "" {
		field("Error", errStyle.Render(j.ParseError))
	}

	if len(m.log) > 0 {
		b.WriteString("\n" + sectionStyle.Render(fmt.Sprintf("─ log tail (%s) ", m.logSrc)) +
			dimStyle.Render(strings.Repeat("─", max(0, m.width-16-len(m.logSrc)))) + "\n")
		for _, l := range m.log {
			b.WriteString(dimStyle.Render(trunc(l, m.width-2)) + "\n")
		}
	} else if j.StdoutPath != "" || j.StderrPath != "" {
		b.WriteString("\n" + dimStyle.Render("(log file empty or unreadable)") + "\n")
	}

	b.WriteString("\n" + m.statusLine())
	b.WriteString(helpStyle.Render("esc/q back · x run now · u load/unload · r refresh"))
	return b.String()
}

func stateDot(j launchd.Job) string {
	switch {
	case j.Running():
		return runningStyle.Render("●")
	case j.StateKnown && j.Loaded && j.LastExit != nil && *j.LastExit != 0:
		return errStyle.Render("●")
	case j.StateKnown && j.Loaded:
		return "●"
	case j.StateKnown:
		return dimStyle.Render("○")
	default:
		return dimStyle.Render("◌")
	}
}

func stateText(j launchd.Job) string {
	switch {
	case j.Running():
		return runningStyle.Render(fmt.Sprintf("running (PID %d)", j.PID))
	case j.StateKnown && j.Loaded && j.LastExit != nil && *j.LastExit != 0:
		return errStyle.Render(fmt.Sprintf("loaded · last exit %d", *j.LastExit))
	case j.StateKnown && j.Loaded:
		return "loaded"
	case j.StateKnown:
		return dimStyle.Render("not loaded")
	default:
		return dimStyle.Render("system domain (root)")
	}
}

// tailLogFor returns the last lines of the job's stdout log,
// falling back to stderr if stdout is missing or empty.
func tailLogFor(j launchd.Job) ([]string, string) {
	if lines := tailFile(j.StdoutPath, 15); len(lines) > 0 {
		return lines, "stdout"
	}
	if lines := tailFile(j.StderrPath, 15); len(lines) > 0 {
		return lines, "stderr"
	}
	return nil, ""
}

func tailFile(path string, n int) []string {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return nil
	}
	const window = 32 * 1024
	offset := int64(0)
	if info.Size() > window {
		offset = info.Size() - window
	}
	buf := make([]byte, min64(window, info.Size()))
	if _, err := f.ReadAt(buf, offset); err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:] // first line is likely cut mid-way
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func shortenHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || p == "" {
		return p
	}
	return strings.Replace(p, home, "~", 1)
}

func trunc(s string, w int) string {
	if w <= 1 || len(s) <= w {
		return s
	}
	return s[:w-1] + "…"
}

func pad(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
