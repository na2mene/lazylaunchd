package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/na2mene/lazylaunchd/internal/launchd"
	"github.com/na2mene/lazylaunchd/internal/power"
)

// tickMsg drives the background auto-refresh.
type tickMsg struct{}

const refreshInterval = 2 * time.Second

func refreshTick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// clockTickMsg re-renders once a second so countdowns tick; it fetches
// nothing — remaining time is computed at render.
type clockTickMsg struct{}

func clockTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return clockTickMsg{} })
}

// followTickMsg drives the log-follow view. seq invalidates stale tick
// chains left over from earlier follow sessions.
type followTickMsg struct{ seq int }

const followInterval = 500 * time.Millisecond

func followTick(seq int) tea.Cmd {
	return tea.Tick(followInterval, func(time.Time) tea.Msg { return followTickMsg{seq} })
}

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
	okBanner     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("16")).Background(lipgloss.Color("42"))
	errBanner    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("124"))
	menuBox      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("39")).Padding(0, 2)
	pathLocStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("45"))
	dirCandStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	logTsStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("73"))
	logErrStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

var (
	logAnsiRe  = regexp.MustCompile("\x1b\\[[0-9;]*m")
	logErrRe   = regexp.MustCompile(`(?i)\b(error|fatal|panic|fail(ed|ure)?)\b`)
	logWarnRe  = regexp.MustCompile(`(?i)\bwarn(ing)?\b`)
	logDebugRe = regexp.MustCompile(`(?i)\b(debug|trace)\b`)
	// ISO datetimes and `date`-style stamps (Wed Aug 19 13:29:24 JST 2026),
	// anywhere in the line — real logs often prefix them with a message.
	logTsRe = regexp.MustCompile(`(\d{4}[-/]\d{2}[-/]\d{2}[ T]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?|\w{3} \w{3} +\d{1,2} \d{2}:\d{2}:\d{2}( \w{3,4})? \d{4})`)
	// Lines that are nothing but a separator rule.
	logSepRe = regexp.MustCompile(`^[=\-─_*┄┈~]{4,}\s*$`)
)

// styleLogLine strips foreign ANSI codes, truncates to width, and applies
// restrained highlighting: level-colored lines, teal leading timestamps.
func styleLogLine(l string, width int) string {
	l = logAnsiRe.ReplaceAllString(l, "")
	l = trunc(l, width)
	switch {
	case logErrRe.MatchString(l):
		return logErrStyle.Render(l)
	case logWarnRe.MatchString(l):
		return warnStyle.Render(l)
	case logDebugRe.MatchString(l):
		return dimStyle.Render(l)
	case logSepRe.MatchString(l):
		return dimStyle.Render(l)
	}
	if loc := logTsRe.FindStringIndex(l); loc != nil {
		return l[:loc[0]] + logTsStyle.Render(l[loc[0]:loc[1]]) + l[loc[1]:]
	}
	return l
}

// sleepImpact answers the key question for a Mac used as an always-on box:
// will this job actually do its work given the current power/sleep state?
func sleepImpact(j launchd.Job, pw power.Status) (string, string) {
	if !j.Timed && !j.KeptAlive {
		return dimStyle.Render("·"), "Not schedule-driven — sleep has no effect on this job."
	}
	if pw.Known && pw.OnAC && pw.SleepPrevented {
		return okStyle.Render("✓"), "The Mac is kept awake (AC + sleep assertion) — runs 24/7, even with the lid closed."
	}
	if j.Timed {
		if pw.Known && !pw.OnAC {
			return errStyle.Render("!"), "On battery, closing the lid sleeps the Mac — scheduled runs stop (one catch-up run on wake)."
		}
		return warnStyle.Render("~"), "No sleep prevention active — runs are skipped while the Mac sleeps (one catch-up run on wake)."
	}
	return warnStyle.Render("~"), "KeepAlive process pauses while the Mac sleeps and resumes on wake."
}

type viewMode int

const (
	listView viewMode = iota
	detailView
	menuView
	wizardView
	logView
)

const (
	actRunFollow = iota
	actRun
	actToggle
	actEditForm
	actDuplicate
	actDelete
	actTruncate
	actFollow
)

const (
	logWarnSize = 50 * 1024 * 1024 // detail view warns above this
	truncateMin = 1024 * 1024      // Truncate keeps 1MB, so below this it's a no-op
)

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0fKB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}

// jobLogSize sums the job's log file sizes (dedup when stdout == stderr).
func jobLogSize(j launchd.Job) int64 {
	var total int64
	seen := map[string]bool{}
	for _, p := range []string{j.StdoutPath, j.StderrPath} {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		if fi, err := os.Stat(p); err == nil {
			total += fi.Size()
		}
	}
	return total
}

func logPathWithSize(p string) string {
	if p == "" {
		return ""
	}
	s := shortenHome(p)
	if fi, err := os.Stat(p); err == nil {
		if fi.Size() > logWarnSize {
			s += warnStyle.Render(fmt.Sprintf(" (⚠ %s — Truncate from the menu)", humanSize(fi.Size())))
		} else {
			s += dimStyle.Render(" (" + humanSize(fi.Size()) + ")")
		}
	}
	return s
}

type menuEntry struct {
	id    int
	label string
	note  string
	ok    bool
}

type confirmState struct {
	prompt   string
	done     string
	run      func() error
	onCancel func()
}

type Model struct {
	jobs       []launchd.Job
	power      power.Status
	cursor     int
	mode       viewMode
	log        []string
	logSrc     string
	status     string
	confirm    *confirmState
	menu       []menuEntry
	menuCursor int
	detailFrom viewMode // where esc/q from the detail view returns to
	logFrom    viewMode // where esc/q from the follow view returns to
	logSize    int64
	followSeq  int
	wiz        *wizard
	hist       *launchd.History
	filter     string
	filtering  bool
	sortNext   bool
	topSel     int // selected button on the top bar (cursor row 0)
	width      int
	height     int
}

// visible returns indices into m.jobs after filtering and sorting.
func (m Model) visible() []int {
	idx := make([]int, 0, len(m.jobs))
	f := strings.ToLower(m.filter)
	for i, j := range m.jobs {
		if f == "" || strings.Contains(strings.ToLower(j.Label), f) {
			idx = append(idx, i)
		}
	}
	if m.sortNext {
		now := time.Now()
		sort.SliceStable(idx, func(a, b int) bool {
			ta, oka := m.jobs[idx[a]].NextRun(now)
			tb, okb := m.jobs[idx[b]].NextRun(now)
			switch {
			case oka && okb:
				return ta.Before(tb)
			case oka:
				return true
			case okb:
				return false
			}
			return m.jobs[idx[a]].Label < m.jobs[idx[b]].Label
		})
	}
	return idx
}

// curJob returns the job under the cursor. Cursor 0 is the "+ New job" row;
// positions 1..n walk the filtered/sorted visible list.
func (m Model) curJob() (launchd.Job, bool) {
	vis := m.visible()
	if m.cursor == 0 || m.cursor > len(vis) {
		return launchd.Job{}, false
	}
	return m.jobs[vis[m.cursor-1]], true
}

func watcherDeclinedMarker() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library/Application Support/lazylaunchd/watcher-declined")
}

func New(jobs []launchd.Job, pw power.Status) Model {
	m := Model{jobs: jobs, power: pw, hist: launchd.LoadHistory()}
	m.hist.Observe(jobs) // baseline snapshot

	// First-run offer: one keypress sets up the background watcher.
	// Declining writes a marker so we never nag again.
	if _, declined := os.Stat(watcherDeclinedMarker()); !launchd.WatcherInstalled() && declined != nil {
		m.confirm = &confirmState{
			prompt: "install the background watcher? it notifies failures while the TUI is closed (y/N)",
			done:   "watcher installed & running — it now watches even when this TUI is closed",
			run: func() error {
				_, err := launchd.SetupWatcher()
				return err
			},
			onCancel: func() {
				os.MkdirAll(filepath.Dir(watcherDeclinedMarker()), 0o755)
				os.WriteFile(watcherDeclinedMarker(), []byte("declined\n"), 0o644)
			},
		}
	}
	return m
}

// watcherAlive reports whether the watcher job is running, judged from the
// already-scanned job list (no extra launchctl calls).
func watcherAlive(jobs []launchd.Job) bool {
	for _, j := range jobs {
		if j.Label == launchd.WatcherLabel {
			return j.Running()
		}
	}
	return false
}

func (m Model) Init() tea.Cmd { return tea.Batch(refreshTick(), clockTick()) }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = ws.Width, ws.Height
		return m, nil
	}
	if _, ok := msg.(clockTickMsg); ok {
		return m, clockTick()
	}
	if ft, ok := msg.(followTickMsg); ok {
		if m.mode == logView && ft.seq == m.followSeq {
			m = m.reloadFollow()
			return m, followTick(ft.seq)
		}
		return m, nil // stale chain: stop re-arming
	}
	if _, ok := msg.(tickMsg); ok {
		// Refresh data only — never cursor, mode, or scroll — so navigation
		// is unaffected. Paused while a menu, confirm, or the wizard is open:
		// the job list must not shift under a pending selection.
		if m.confirm == nil && (m.mode == listView || m.mode == detailView) {
			m = m.rescan()
		}
		return m, refreshTick()
	}
	if m.mode == wizardView && m.wiz != nil {
		return m.updateWizard(msg)
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.confirm != nil {
			switch msg.String() {
			case "y", "Y":
				c := m.confirm
				m.confirm = nil
				if err := c.run(); err != nil {
					m.status = errBanner.Render(" ✗ " + err.Error() + " ")
				} else {
					m.status = okBanner.Render(" ✓ " + c.done + " ")
					if m.mode == menuView {
						m.mode = listView // the job the menu belonged to is gone
					}
				}
				m = m.rescan()
			case "ctrl+c":
				return m, tea.Quit
			default:
				c := m.confirm
				m.confirm = nil
				m.status = dimStyle.Render("canceled")
				if c.onCancel != nil {
					c.onCancel()
				}
			}
			return m, nil
		}
		if m.mode == logView {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc", "q":
				m.mode = m.logFrom
			case "t":
				if j, ok := m.curJob(); ok && j.StdoutPath != "" && j.StderrPath != "" && j.StdoutPath != j.StderrPath {
					if m.logSrc == "stdout" {
						m.logSrc = "stderr"
					} else {
						m.logSrc = "stdout"
					}
					m.logSize = -1
					m = m.reloadFollow()
				}
			}
			return m, nil
		}
		if m.mode == menuView {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc", "q":
				m.mode = listView
			case "j", "down":
				if m.menuCursor < len(m.menu)-1 {
					m.menuCursor++
				}
			case "k", "up":
				if m.menuCursor > 0 {
					m.menuCursor--
				}
			case "enter":
				return m.execMenu()
			}
			return m, nil
		}
		if m.filtering {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.filtering = false
				m.filter = ""
			case "enter":
				m.filtering = false
			case "backspace":
				if len(m.filter) > 0 {
					r := []rune(m.filter)
					m.filter = string(r[:len(r)-1])
				}
			default:
				if msg.Type == tea.KeyRunes {
					m.filter += string(msg.Runes)
				}
			}
			if m.cursor > len(m.visible()) {
				m.cursor = len(m.visible())
			}
			return m, nil
		}
		m.status = ""
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.mode == detailView {
				m.mode = m.detailFrom
				return m, nil
			}
			return m, tea.Quit
		case "esc":
			if m.mode == detailView {
				m.mode = m.detailFrom
			} else if m.filter != "" {
				m.filter = ""
			}
		case "/":
			if m.mode == listView {
				m.filtering = true
				m.filter = ""
				m.cursor = 0
			}
		case "s":
			if m.mode == listView {
				m.sortNext = !m.sortNext
			}
		case "j", "down":
			if m.mode == listView && m.cursor < len(m.visible()) {
				m.cursor++
			}
		case "k", "up":
			if m.mode == listView && m.cursor > 0 {
				m.cursor--
			}
		case "h", "left":
			if m.mode == listView && m.cursor == 0 && m.topSel > 0 {
				m.topSel--
			}
		case "l", "right":
			if m.mode == listView && m.cursor == 0 && m.topSel < 2 {
				m.topSel++
			}
		case "g":
			m.cursor = 0
		case "G":
			m.cursor = len(m.visible())
		case "n":
			if m.mode == listView {
				return m.startWizard()
			}
		case "enter":
			if m.mode != listView {
				break
			}
			if m.cursor == 0 {
				switch m.topSel {
				case 1:
					m.filtering = true
					m.filter = ""
				case 2:
					m.sortNext = !m.sortNext
				default:
					return m.startWizard()
				}
				break
			}
			if j, ok := m.curJob(); ok {
				m.mode = menuView
				m.menu = buildMenu(j)
				m.menuCursor = 0
			}
		case "d":
			if j, ok := m.curJob(); ok {
				m.mode = detailView
				m.detailFrom = listView
				m.log, m.logSrc = tailLogFor(j)
			}
		case "f":
			if m.mode == listView || m.mode == detailView {
				return m.startFollow(m.mode)
			}
		case "e":
			if j, ok := m.curJob(); ok && (m.mode == listView || m.mode == detailView) {
				if j.Kind != launchd.UserAgent {
					m.status = errBanner.Render(" ✗ only user agents can be edited (system files need root) ")
					break
				}
				return m.startEditForm(j)
			}
		case "x":
			if j, ok := m.curJob(); ok {
				if err := launchd.RunNow(j); err != nil {
					m.status = errStyle.Render(err.Error())
				} else {
					m.status = okStyle.Render("started: " + j.Label)
				}
				m = m.rescan()
			}
		case "u":
			if j, ok := m.curJob(); ok {
				switch {
				case j.Kind == launchd.Daemon:
					m.status = errStyle.Render("system daemons need root — use sudo launchctl")
				case j.Loaded:
					m.confirm = &confirmState{
						prompt: fmt.Sprintf("disable %s? stops it now and keeps it off after restarts (y/N)", j.Label),
						done:   "disabled: " + j.Label,
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

func buildMenu(j launchd.Job) []menuEntry {
	rootNote := ""
	actionable := j.Kind != launchd.Daemon
	if !actionable {
		rootNote = "needs root"
	}
	toggle := menuEntry{id: actToggle, label: "Enable — start now, runs at login again", ok: actionable, note: rootNote}
	if j.Loaded {
		toggle.label = "Disable — stop now, stays off after restart"
	}
	del := menuEntry{id: actDelete, label: "Delete — unload & move plist + logs to Trash", ok: j.Kind == launchd.UserAgent}
	if !del.ok {
		del.note = "user agents only"
	}
	hasLogs := j.StdoutPath != "" || j.StderrPath != ""
	follow := menuEntry{id: actFollow, label: "Log (tail -f)", ok: hasLogs}
	if !follow.ok {
		follow.note = "no log paths"
	}
	runFollow := menuEntry{id: actRunFollow, label: "Run now & follow log", ok: actionable && hasLogs, note: rootNote}
	if actionable && !hasLogs {
		runFollow.note = "no log paths"
	}
	editForm := menuEntry{id: actEditForm, label: "Edit — form, like New job", ok: j.Kind == launchd.UserAgent}
	if !editForm.ok {
		editForm.note = "user agents only"
	}
	truncEntry := menuEntry{id: actTruncate, label: "Truncate logs — keep last 1MB"}
	switch total := jobLogSize(j); {
	case !hasLogs:
		truncEntry.note = "no log paths"
	case total <= truncateMin:
		truncEntry.note = fmt.Sprintf("only %s — nothing to trim", humanSize(total))
	default:
		truncEntry.label = fmt.Sprintf("Truncate logs — keep last 1MB (now %s)", humanSize(total))
		truncEntry.ok = true
	}
	dup := menuEntry{id: actDuplicate, label: "Duplicate — new job based on this one", ok: len(j.Program) > 0}
	if !dup.ok {
		dup.note = "no program to copy"
	}
	return []menuEntry{
		runFollow,
		{id: actRun, label: "Run now (stay here)", note: rootNote, ok: actionable},
		toggle,
		editForm,
		dup,
		del,
		truncEntry,
		follow,
	}
}

func (m Model) execMenu() (Model, tea.Cmd) {
	j, ok := m.curJob()
	if !ok || m.menuCursor >= len(m.menu) {
		m.mode = listView
		return m, nil
	}
	entry := m.menu[m.menuCursor]

	if !entry.ok {
		m.mode = listView
		m.status = errBanner.Render(" ✗ " + entry.label + ": " + entry.note + " ")
		return m, nil
	}

	switch entry.id {
	case actRunFollow:
		if err := launchd.RunNow(j); err != nil {
			m.mode = listView
			m.status = errBanner.Render(" ✗ " + err.Error() + " ")
			m = m.rescan()
			return m, nil
		}
		m = m.rescan()
		return m.startFollow(listView)
	case actRun:
		m.mode = listView
		if err := launchd.RunNow(j); err != nil {
			m.status = errBanner.Render(" ✗ " + err.Error() + " ")
		} else {
			m.status = okBanner.Render(" ✓ started: " + j.Label + " ")
		}
		m = m.rescan()
	case actToggle:
		m.mode = listView
		var err error
		verb := "enabled (runs at login again)"
		if j.Loaded {
			verb = "disabled (stays off after restart)"
			err = launchd.Unload(j)
		} else {
			err = launchd.Load(j)
		}
		if err != nil {
			m.status = errBanner.Render(" ✗ " + err.Error() + " ")
		} else {
			m.status = okBanner.Render(" ✓ " + verb + ": " + j.Label + " ")
		}
		m = m.rescan()
	case actDelete:
		m.confirm = &confirmState{
			prompt: fmt.Sprintf("delete %s? unloads it, moves plist + logs to Trash (y/N)", j.Label),
			done:   "deleted (plist in Trash): " + j.Label,
			run:    func() error { return launchd.Delete(j) },
		}
	case actTruncate:
		m.confirm = &confirmState{
			prompt: fmt.Sprintf("truncate logs of %s? keeps only the last 1MB (y/N)", j.Label),
			done:   "logs truncated: " + j.Label,
			run:    func() error { return launchd.TruncateLogs(j) },
		}
	case actEditForm:
		return m.startEditForm(j)
	case actDuplicate:
		return m.startDuplicate(j)
	case actFollow:
		return m.startFollow(menuView)
	}
	return m, nil
}

// startFollow opens the tail -f view for the job under the cursor.
func (m Model) startFollow(from viewMode) (Model, tea.Cmd) {
	j, ok := m.curJob()
	if !ok {
		return m, nil
	}
	if j.StdoutPath == "" && j.StderrPath == "" {
		m.status = errBanner.Render(" ✗ no log paths defined for " + j.Label + " ")
		return m, nil
	}
	m.logSrc = "stdout"
	if j.StdoutPath == "" {
		m.logSrc = "stderr"
	}
	m.mode = logView
	m.logFrom = from
	m.logSize = -1
	m.followSeq++
	m = m.reloadFollow()
	return m, followTick(m.followSeq)
}

// followPath is the log file the follow view currently streams.
func (m Model) followPath() string {
	j, ok := m.curJob()
	if !ok {
		return ""
	}
	if m.logSrc == "stderr" {
		return j.StderrPath
	}
	return j.StdoutPath
}

// reloadFollow rereads the tail, skipping the read when the size is unchanged.
func (m Model) reloadFollow() Model {
	path := m.followPath()
	if fi, err := os.Stat(path); err == nil {
		if fi.Size() == m.logSize {
			return m
		}
		m.logSize = fi.Size()
	}
	m.log = tailFile(path, 300)
	return m
}

// logPanel renders the full-screen tail -f view.
func (m Model) logPanel() string {
	j, ok := m.curJob()
	if !ok {
		return m.list()
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(j.Label) + "  " + okStyle.Render("● following") + dimStyle.Render(" · "+m.logSrc+" · 0.5s") + "\n")
	b.WriteString(dimStyle.Render(shortenHome(m.followPath())) + "\n")
	b.WriteString(dimStyle.Render(strings.Repeat("─", max(10, m.width-2))) + "\n")

	avail := m.height - 5
	if avail < 3 {
		avail = 3
	}
	lines := m.log
	if len(lines) > avail {
		lines = lines[len(lines)-avail:]
	}
	if len(lines) == 0 {
		b.WriteString(dimStyle.Render("(empty — waiting for output…)") + "\n")
	}
	for _, l := range lines {
		b.WriteString(styleLogLine(l, m.width-1) + "\n")
	}
	b.WriteString("\n" + helpStyle.Render("t stdout/stderr · esc back"))
	return b.String()
}

// rescan refreshes jobs, power state, and (in detail view) the log tail.
// It also feeds the run-history observer and fires failure notifications.
func (m Model) rescan() Model {
	if jobs, err := launchd.Scan(); err == nil {
		m.jobs = jobs
	}
	m.power = power.Read()
	if m.hist != nil {
		if watcherAlive(m.jobs) {
			// The watcher owns observation and notifications; just read
			// what it recorded and keep our snapshots current.
			m.hist.Reload()
			m.hist.Baseline(m.jobs)
		} else {
			for _, f := range m.hist.Observe(m.jobs) {
				launchd.Notify("lazylaunchd", fmt.Sprintf("%s failed (exit %d)", f.Label, f.Exit))
				m.status = errBanner.Render(fmt.Sprintf(" ✗ %s failed (exit %d) ", f.Label, f.Exit))
			}
		}
	}
	if m.cursor > len(m.visible()) {
		m.cursor = len(m.visible())
	}
	if m.mode == detailView {
		if j, ok := m.curJob(); ok {
			m.log, m.logSrc = tailLogFor(j)
		}
	}
	return m
}

func (m Model) View() string {
	if m.width == 0 {
		// No WindowSizeMsg yet (or a pty that reports no size): use a sane default.
		m.width, m.height = 80, 24
	}
	switch m.mode {
	case detailView:
		return m.detail()
	case menuView:
		return m.menuPanel()
	case wizardView:
		return m.wizardView()
	case logView:
		return m.logPanel()
	default:
		return m.list()
	}
}

// menuPanel shows the job's full information plus the selectable actions.
func (m Model) menuPanel() string {
	j, ok := m.curJob()
	if !ok {
		return m.list()
	}
	var b strings.Builder

	b.WriteString(titleStyle.Render(j.Label) + "\n")
	b.WriteString(dimStyle.Render(j.Kind.String()) + "  " + m.stateCell(j) + "\n\n")
	b.WriteString(m.jobInfo(j) + "\n")

	var items strings.Builder
	for i, e := range m.menu {
		label := e.label
		if e.note != "" {
			label += "  (" + e.note + ")"
		}
		switch {
		case i == m.menuCursor:
			items.WriteString(cursorStyle.Render("▸ "+label) + "\n")
		case !e.ok:
			items.WriteString(dimStyle.Render("  "+label) + "\n")
		default:
			items.WriteString("  " + label + "\n")
		}
	}
	b.WriteString(menuBox.Render(strings.TrimRight(items.String(), "\n")) + "\n")

	b.WriteString("\n" + m.statusLine())
	b.WriteString(helpStyle.Render("j/k move · enter select · esc cancel"))
	return b.String()
}

type row struct {
	header string
	job    int // index into m.jobs, -1 for headers
}

func (m Model) list() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("lazylaunchd") + "  " + dimStyle.Render(m.power.Headline()) + "\n")
	b.WriteString(dimStyle.Render("  sleep: ") + okStyle.Render("✓") + dimStyle.Render(" runs 24/7 · ") +
		warnStyle.Render("~") + dimStyle.Render(" skipped/paused while asleep · ") +
		errStyle.Render("!") + dimStyle.Render(" stops on battery lid-close") + "\n\n")

	usedLines := 3 // title + legend + blank
	if m.filtering {
		b.WriteString(confirmStyle.Render(" / "+m.filter+"▌ ") + helpStyle.Render("  type to filter · enter keep · esc clear") + "\n")
		usedLines++
	} else if m.filter != "" {
		b.WriteString(dimStyle.Render("  filter: "+m.filter+" (esc to clear)") + "\n")
		usedLines++
	}

	vis := m.visible()
	rows := []row{{job: -2}} // "+ New job" row at the top
	if m.sortNext {
		rows = append(rows, row{header: fmt.Sprintf("By next run (%d) — s to restore groups", len(vis)), job: -1})
		for _, i := range vis {
			rows = append(rows, row{job: i})
		}
	} else {
		counts := map[launchd.Kind]int{}
		for _, i := range vis {
			counts[m.jobs[i].Kind]++
		}
		lastKind := launchd.Kind(-1)
		for _, i := range vis {
			j := m.jobs[i]
			if j.Kind != lastKind {
				rows = append(rows, row{header: fmt.Sprintf("%s (%d)", j.Kind, counts[j.Kind]), job: -1})
				lastKind = j.Kind
			}
			rows = append(rows, row{job: i})
		}
	}

	// The cursor addresses the visible list; map it to the underlying index.
	target := -2
	if m.cursor > 0 && m.cursor <= len(vis) {
		target = vis[m.cursor-1]
	}

	// Simple scrolling: keep the cursor row visible.
	avail := m.height - usedLines - 4
	if avail < 3 {
		avail = 3
	}
	cursorRow := 0
	for ri, r := range rows {
		if r.job == target {
			cursorRow = ri
		}
	}
	start := 0
	if len(rows) > avail && cursorRow >= avail-1 {
		start = cursorRow - avail + 2
	}
	end := min(len(rows), start+avail)

	// Column budget: fixed state/next/history columns, the rest split
	// between label and schedule so narrow terminals degrade gracefully.
	const stateW, nextW, histW = 22, 17, 5
	rest := m.width - stateW - nextW - histW - 14 // icons + gaps
	labelW := clamp(rest*55/100, 20, 48)
	schedW := clamp(rest-labelW, 14, 34)

	for _, r := range rows[start:end] {
		if r.job == -1 {
			b.WriteString(sectionStyle.Render(r.header) + "\n")
			continue
		}
		if r.job == -2 {
			seg := func(i int, label string) string {
				if target == -2 && m.topSel == i {
					return cursorStyle.Render(" ▸ " + label + " ")
				}
				return "   " + label + " "
			}
			sortLabel := "Sort: groups (s)"
			if m.sortNext {
				sortLabel = "Sort: next run (s)"
			}
			b.WriteString(" " + seg(0, "+ New job (n)") + seg(1, "Search (/)") + seg(2, sortLabel) + "\n")
			continue
		}
		j := m.jobs[r.job]
		icon, _ := sleepImpact(j, m.power)
		next := ""
		if t, ok := j.NextRun(time.Now()); ok {
			if d := time.Until(t); d < 24*time.Hour {
				next = fmt.Sprintf("→%s (%s)", t.Format("15:04"), shortDur(d))
			} else {
				next = "→" + t.Format("01-02 15:04")
			}
		}
		line := fmt.Sprintf("  %s %s %s  %s  %s  %s  %s",
			icon,
			stateDot(j),
			pad(truncMid(j.Label, labelW), labelW),
			pad(trunc(j.Schedule, schedW), schedW),
			pad(next, nextW),
			m.histCell(j.Label),
			m.stateCell(j),
		)
		if r.job == target {
			line = cursorStyle.Render("▸" + line[1:])
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n" + m.statusLine())
	b.WriteString(helpStyle.Render("enter info & actions · n new · e edit · f log · / filter · s sort by next run · j/k · q quit"))
	return b.String()
}

// jobInfo renders the job's fields — shared by the detail view and the
// action menu, so the facts stay visible while choosing an action.
func (m Model) jobInfo(j launchd.Job) string {
	var b strings.Builder
	field := func(name, val string) {
		if val == "" {
			val = dimStyle.Render("—")
		}
		b.WriteString(fmt.Sprintf("%s %s\n", sectionStyle.Render(pad(name+":", 10)), val))
	}
	field("Plist", shortenHome(j.PlistPath))
	field("Program", trunc(strings.Join(j.Program, " "), m.width-14))
	if j.EnvPATH != "" {
		field("Env PATH", trunc(j.EnvPATH, max(20, m.width-14)))
	}
	if j.WorkDir != "" {
		field("Workdir", shortenHome(j.WorkDir))
	}
	field("Schedule", j.Schedule)
	if t, ok := j.NextRun(time.Now()); ok {
		field("Next run", fmt.Sprintf("%s (in %s)", t.Format("2006-01-02 15:04"), humanDur(time.Until(t))))
	} else if j.IntervalBased() {
		field("Next run", dimStyle.Render("interval timer — counts from its last fire; launchd doesn't expose the next time"))
	}
	sleepIcon, sleepNote := sleepImpact(j, m.power)
	field("Sleep", sleepIcon+" "+sleepNote)
	if runs := m.hist.Runs(j.Label); len(runs) > 0 {
		last := runs
		if len(last) > 5 {
			last = last[len(last)-5:]
		}
		var parts []string
		for i := len(last) - 1; i >= 0; i-- {
			r := last[i]
			mark := okStyle.Render("● ok")
			if r.Exit != 0 {
				mark = errStyle.Render(fmt.Sprintf("✗ exit %d", r.Exit))
			}
			parts = append(parts, r.At.Format("01-02 15:04")+" "+mark)
		}
		field("History", strings.Join(parts, dimStyle.Render("  ·  ")))
	} else if j.StateKnown {
		field("History", dimStyle.Render("no runs observed yet — recorded while lazylaunchd is open"))
	}
	field("Stdout", logPathWithSize(j.StdoutPath))
	field("Stderr", logPathWithSize(j.StderrPath))
	if due, ok := m.hist.Stale(j, time.Now()); ok {
		field("Alert", warnStyle.Render(fmt.Sprintf("⚠ missed its %s run — was due but never observed", due.Format("15:04"))))
	}
	if j.ParseError != "" {
		field("Error", errStyle.Render(j.ParseError))
	}
	return b.String()
}

// stateCell is stateText plus the missed-run alert, which outranks it.
func (m Model) stateCell(j launchd.Job) string {
	if due, ok := m.hist.Stale(j, time.Now()); ok {
		return warnStyle.Render("⚠ missed " + due.Format("15:04"))
	}
	return stateText(j)
}

// histCell renders the last five observed runs, newest on the right.
func (m Model) histCell(label string) string {
	if m.hist == nil {
		return strings.Repeat(" ", 5)
	}
	runs := m.hist.Runs(label)
	if len(runs) > 5 {
		runs = runs[len(runs)-5:]
	}
	var sb strings.Builder
	for i := 0; i < 5-len(runs); i++ {
		sb.WriteString(dimStyle.Render("·"))
	}
	for _, r := range runs {
		if r.Exit == 0 {
			sb.WriteString(okStyle.Render("●"))
		} else {
			sb.WriteString(errStyle.Render("✗"))
		}
	}
	return sb.String()
}

// statusLine renders the pending confirmation or the last action result.
func (m Model) statusLine() string {
	if m.confirm != nil {
		return confirmStyle.Render(" "+m.confirm.prompt+" ") + "\n"
	}
	if m.status != "" {
		return m.status + "\n"
	}
	return "\n"
}

func (m Model) detail() string {
	j, ok := m.curJob()
	if !ok {
		return m.list()
	}
	var b strings.Builder

	b.WriteString(titleStyle.Render(j.Label) + "\n")
	b.WriteString(dimStyle.Render(j.Kind.String()) + "  " + m.stateCell(j) + "\n\n")
	b.WriteString(m.jobInfo(j))

	if len(m.log) > 0 {
		b.WriteString("\n" + sectionStyle.Render(fmt.Sprintf("─ log tail (%s) ", m.logSrc)) +
			dimStyle.Render(strings.Repeat("─", max(0, m.width-16-len(m.logSrc)))) + "\n")
		for _, l := range m.log {
			b.WriteString(styleLogLine(l, m.width-2) + "\n")
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
	case j.StateKnown && j.Disabled:
		return warnStyle.Render("⊘")
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
	case j.StateKnown && j.Disabled:
		return warnStyle.Render("⊘ disabled")
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
	const window = 128 * 1024
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

// pad aligns by display width, not bytes — labels and the next-run arrow
// contain multibyte runes.
func pad(s string, w int) string {
	d := w - lipgloss.Width(s)
	if d <= 0 {
		return s
	}
	return s + strings.Repeat(" ", d)
}

// truncMid keeps head and tail — for reverse-DNS labels both the vendor
// prefix and the job name at the end carry meaning.
func truncMid(s string, w int) string {
	if w <= 3 || len(s) <= w {
		return s
	}
	head := (w - 1) / 2
	tail := w - 1 - head
	return s[:head] + "…" + s[len(s)-tail:]
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// shortDur is a ticking countdown for the list column: 29:59, or 3:04:59
// with an hour part.
func shortDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	h := total / 3600
	mnt := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, mnt, s)
	}
	return fmt.Sprintf("%02d:%02d", mnt, s)
}

func humanDur(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Minute {
		return "under 1m"
	}
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	mnt := int(d.Minutes()) % 60
	out := ""
	if days > 0 {
		out += fmt.Sprintf("%dd", days)
	}
	if h > 0 {
		out += fmt.Sprintf("%dh", h)
	}
	if mnt > 0 && days == 0 {
		out += fmt.Sprintf("%dm", mnt)
	}
	if out == "" {
		out = "0m"
	}
	return out
}
