package ui

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/na2mene/lazylaunchd/internal/launchd"
)

// Wizard steps, in order.
const (
	wScript = iota
	wLabel
	wSchedType
	wSchedValue
	wLogDir
	wEnvPath
	wConfirm
)

// launchd hands jobs a bare PATH (/usr/bin:/bin:/usr/sbin:/sbin), which
// hides every Homebrew-installed tool. This default fixes that.
const defaultEnvPath = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

var schedOptions = []struct {
	label string
	kind  int
	hint  string
}{
	// Ordered by how often the job fires, shortest interval first;
	// the non-scheduled pair sits at the bottom.
	{"Every N minutes", launchd.SchedInterval, "minutes, e.g. 15"},
	{"Hourly at xx:MM", launchd.SchedHourly, "minute 0-59, e.g. 30"},
	{"Daily at HH:MM", launchd.SchedDaily, "e.g. 09:30"},
	{"Weekly on a day at HH:MM", launchd.SchedWeekly, "day + time, e.g. mon 09:30"},
	{"Once at a specific date/time", launchd.SchedOnce, "MM-DD HH:MM, e.g. 01-01 15:00"},
	{"Always on (KeepAlive)", launchd.SchedKeepAlive, ""},
	{"Manual only", launchd.SchedManual, ""},
}

var confirmOptions = []string{"Save & load now", "Save only (load later with u)", "Cancel"}
var editConfirmOptions = []string{"Save & apply (reload)", "Save only (apply later)", "Cancel"}

type wizard struct {
	step       int
	input      textinput.Model
	schedSel   int
	confirmSel int

	scriptRaw   string
	program     []string
	prefix      string
	suffixRaw   string
	label       string
	schedKind   int
	schedValRaw string
	minute      int
	hour        int
	month       int
	day         int
	weekday     int
	intervalMin int
	logDirRaw   string
	logDir      string
	envPathRaw  string
	envSeed     string
	preview     string
	errMsg      string
	completions []string

	editing      bool
	orig         launchd.Job
	logSeed      string // log dir as prefilled; unchanged → keep original paths
	schedComplex bool   // original schedule doesn't map onto the presets
}

func newWizard() *wizard {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 250
	ti.Width = 60
	prefix := "com.user."
	if u, err := user.Current(); err == nil && u.Username != "" {
		prefix = "com." + u.Username + "."
	}
	w := &wizard{input: ti, prefix: prefix, schedKind: -1}
	w.envPathRaw = defaultEnvPath
	w.envSeed = defaultEnvPath
	w.prepInput()
	return w
}

// prepInput sets the text input up for the current step.
func (w *wizard) prepInput() {
	switch w.step {
	case wScript:
		w.input.Placeholder = "/path/to/script.sh"
		w.input.SetValue(w.scriptRaw)
	case wLabel:
		w.input.Placeholder = "my-job"
		w.input.SetValue(w.suffixRaw)
	case wSchedValue:
		w.input.Placeholder = schedOptions[w.schedSel].hint
		w.input.SetValue(w.schedValRaw)
	case wLogDir:
		if w.logDirRaw == "" {
			w.logDirRaw = "~/Library/Logs/" + w.label
		}
		w.input.SetValue(w.logDirRaw)
	case wEnvPath:
		w.input.Placeholder = defaultEnvPath
		w.input.SetValue(w.envPathRaw)
	}
	w.input.CursorEnd()
	if w.step == wScript || w.step == wLogDir {
		w.completions = listCandidates(w.input.Value())
	} else {
		w.completions = nil
	}
}

func (w *wizard) hasValueStep() bool {
	return w.schedKind != launchd.SchedKeepAlive && w.schedKind != launchd.SchedManual
}

func (w *wizard) prevStep() int {
	switch w.step {
	case wSchedType:
		if w.editing {
			return wScript // label is fixed while editing
		}
		return wLabel
	case wLogDir:
		if w.hasValueStep() {
			return wSchedValue
		}
		return wSchedType
	case wEnvPath:
		return wLogDir
	case wConfirm:
		return wEnvPath
	default:
		return w.step - 1
	}
}

func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// completionDir is the directory tab completion currently searches,
// shown to the user as the "current location".
func completionDir(val string) string {
	expanded := expandTilde(val)
	if strings.HasSuffix(val, "/") && !strings.HasSuffix(expanded, "/") {
		expanded += "/"
	}
	dir, _ := filepath.Split(expanded)
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			return wd + "/"
		}
		return "./"
	}
	return dir
}

// renderCandidates lays completion candidates out in bash-style columns.
func renderCandidates(cands []string, width int) string {
	colw := 0
	for _, c := range cands {
		if w := lipgloss.Width(c); w > colw {
			colw = w
		}
	}
	colw += 2
	ncols := max(1, (width-4)/colw)
	const maxRows = 5
	shown := cands
	more := 0
	if len(shown) > ncols*maxRows {
		more = len(shown) - ncols*maxRows
		shown = shown[:ncols*maxRows]
	}
	var sb strings.Builder
	for i, c := range shown {
		if i%ncols == 0 {
			sb.WriteString("  ")
		}
		disp := c
		if strings.HasSuffix(c, "/") {
			disp = dirCandStyle.Render(c) // directories in blue, ls-style
		}
		d := colw - lipgloss.Width(c)
		sb.WriteString(disp + strings.Repeat(" ", max(0, d)))
		if (i+1)%ncols == 0 {
			sb.WriteString("\n")
		}
	}
	if len(shown)%ncols != 0 {
		sb.WriteString("\n")
	}
	if more > 0 {
		sb.WriteString(dimStyle.Render(fmt.Sprintf("  … +%d more", more)) + "\n")
	}
	return sb.String()
}

// splitForCompletion resolves the directory and partial name the input
// currently points at. filepath.Join in expandTilde drops a trailing slash;
// restore it so "~/" completes inside the home directory, not on "~" itself.
func splitForCompletion(val string) (string, string) {
	expanded := expandTilde(val)
	if strings.HasSuffix(val, "/") && !strings.HasSuffix(expanded, "/") {
		expanded += "/"
	}
	dir, base := filepath.Split(expanded)
	if dir == "" {
		dir = "./"
	}
	return dir, base
}

// listCandidates returns the entries matching the input as typed so far.
func listCandidates(val string) []string {
	dir, base := splitForCompletion(val)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var matches []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, base) {
			continue
		}
		if base == "" && strings.HasPrefix(name, ".") {
			continue // hidden files only when explicitly typed
		}
		if e.IsDir() {
			name += "/"
		}
		matches = append(matches, name)
	}
	return matches
}

// completePath does shell-style tab completion: extends the value to the
// longest common prefix of matching entries.
func completePath(val string) (string, []string) {
	matches := listCandidates(val)
	if len(matches) == 0 {
		return val, nil
	}
	dir, base := splitForCompletion(val)

	lcp := matches[0]
	for _, s := range matches[1:] {
		for !strings.HasPrefix(s, lcp) {
			lcp = lcp[:len(lcp)-1]
		}
	}
	lcp = strings.TrimSuffix(lcp, "/")
	if len(matches) == 1 {
		lcp = matches[0] // keep the trailing slash for a unique directory
	}

	newVal := filepath.Join(dir, lcp)
	if strings.HasSuffix(lcp, "/") {
		newVal += "/"
	}
	if strings.HasSuffix(val, base) { // preserve the ~ the user typed
		newVal = strings.TrimSuffix(val, base) + lcp
	}
	if len(matches) == 1 {
		return newVal, nil
	}
	return newVal, matches
}

func (w *wizard) newJob() launchd.NewJob {
	nj := launchd.NewJob{
		Label:       w.label,
		Program:     w.program,
		SchedKind:   w.schedKind,
		Minute:      w.minute,
		Hour:        w.hour,
		Month:       w.month,
		Day:         w.day,
		Weekday:     w.weekday,
		IntervalMin: w.intervalMin,
		StdoutPath:  filepath.Join(w.logDir, "stdout.log"),
		StderrPath:  filepath.Join(w.logDir, "stderr.log"),
	}
	// Editing with the log dir untouched: keep the original file names —
	// never silently rename someone's log files.
	if w.editing && w.logSeed != "" && w.logDirRaw == w.logSeed {
		nj.StdoutPath = w.orig.StdoutPath
		nj.StderrPath = w.orig.StderrPath
	}
	nj.EnvPath = w.envPathRaw
	nj.EnvPathSet = w.envPathRaw != w.envSeed
	return nj
}

func (m Model) startWizard() (Model, tea.Cmd) {
	m.mode = wizardView
	m.wiz = newWizard()
	m.status = ""
	return m, textinput.Blink
}

// startEditForm opens the wizard prefilled with an existing job's values.
func (m Model) startEditForm(j launchd.Job) (Model, tea.Cmd) {
	m.mode = wizardView
	m.status = ""
	w := newWizard()
	w.editing = true
	w.orig = j
	w.label = j.Label
	w.scriptRaw = j.CommandSeed()
	if kind, val, ok := j.EditSeed(); ok {
		for i, opt := range schedOptions {
			if opt.kind == kind {
				w.schedSel = i
			}
		}
		w.schedValRaw = val
	} else {
		w.schedComplex = true
	}
	if j.StdoutPath != "" {
		w.logDirRaw = shortenHome(filepath.Dir(j.StdoutPath))
		w.logSeed = w.logDirRaw
	}
	// Never add a PATH to an existing job behind the user's back: seed with
	// whatever the plist has now (possibly nothing).
	w.envPathRaw = j.EnvPATH
	w.envSeed = j.EnvPATH
	w.prepInput()
	m.wiz = w
	return m, textinput.Blink
}

func (m Model) updateWizard(msg tea.Msg) (tea.Model, tea.Cmd) {
	w := m.wiz
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			w.errMsg = ""
			if w.step == wScript {
				m.mode = listView
				m.wiz = nil
				return m, nil
			}
			w.step = w.prevStep()
			w.prepInput()
			return m, nil
		case "enter":
			return m.wizardEnter()
		case "tab":
			if w.step == wScript || w.step == wLogDir {
				val, _ := completePath(w.input.Value())
				w.input.SetValue(val)
				w.input.CursorEnd()
				w.completions = listCandidates(val)
			}
			if w.step == wEnvPath {
				w.input.SetValue(defaultEnvPath)
				w.input.CursorEnd()
			}
			return m, nil
		}
		if w.step == wSchedType {
			switch key.String() {
			case "j", "down":
				if w.schedSel < len(schedOptions)-1 {
					w.schedSel++
				}
			case "k", "up":
				if w.schedSel > 0 {
					w.schedSel--
				}
			}
			return m, nil
		}
		if w.step == wConfirm {
			switch key.String() {
			case "j", "down":
				if w.confirmSel < len(confirmOptions)-1 {
					w.confirmSel++
				}
			case "k", "up":
				if w.confirmSel > 0 {
					w.confirmSel--
				}
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	w.input, cmd = w.input.Update(msg)
	if w.step == wScript || w.step == wLogDir {
		w.completions = listCandidates(w.input.Value()) // live filter as you type
	}
	return m, cmd
}

func (m Model) wizardEnter() (tea.Model, tea.Cmd) {
	w := m.wiz
	w.errMsg = ""

	switch w.step {
	case wScript:
		raw := strings.TrimSpace(w.input.Value())
		if raw == "" {
			w.errMsg = "path is required"
			return m, nil
		}
		switch {
		case w.editing && raw == w.orig.CommandSeed():
			// Untouched: keep the exact original arguments.
			w.program = w.orig.Program
		default:
			path := expandTilde(raw)
			// launchd starts jobs with CWD=/ — a relative path in the
			// plist would never resolve, so pin it down now.
			if abs, err := filepath.Abs(path); err == nil {
				path = abs
			}
			info, err := os.Stat(path)
			switch {
			case err == nil && info.IsDir():
				w.errMsg = "that is a directory"
				return m, nil
			case err == nil && info.Mode()&0o111 != 0:
				w.program = []string{path}
			case err == nil:
				w.program = []string{"/bin/sh", path} // not executable: run through sh
			case w.editing && strings.Contains(raw, " "):
				w.program = []string{"/bin/sh", "-c", raw} // free-form command
			default:
				w.errMsg = "file not found: " + path
				return m, nil
			}
		}
		w.scriptRaw = raw
		if w.editing {
			w.step = wSchedType // label is fixed while editing
		} else {
			w.step = wLabel
		}
		w.prepInput()

	case wLabel:
		suffix := strings.TrimSpace(w.input.Value())
		if suffix == "" {
			w.errMsg = "name is required"
			return m, nil
		}
		if !regexp.MustCompile(`^[A-Za-z0-9._-]+$`).MatchString(suffix) {
			w.errMsg = "use letters, digits, . _ - only"
			return m, nil
		}
		w.suffixRaw = suffix
		w.label = w.prefix + suffix
		if _, err := os.Stat((launchd.NewJob{Label: w.label}).PlistPath()); err == nil {
			w.errMsg = w.label + " already exists"
			return m, nil
		}
		w.step = wSchedType

	case wSchedType:
		w.schedKind = schedOptions[w.schedSel].kind
		if w.hasValueStep() {
			w.step = wSchedValue
		} else {
			w.step = wLogDir
		}
		w.prepInput()

	case wSchedValue:
		raw := strings.TrimSpace(w.input.Value())
		if err := w.parseSchedValue(raw); err != nil {
			w.errMsg = err.Error()
			return m, nil
		}
		w.schedValRaw = raw
		w.step = wLogDir
		w.prepInput()

	case wLogDir:
		raw := strings.TrimSpace(w.input.Value())
		if raw == "" {
			w.errMsg = "log directory is required"
			return m, nil
		}
		w.logDirRaw = raw
		w.logDir = expandTilde(raw)
		w.step = wEnvPath
		w.prepInput()

	case wEnvPath:
		w.envPathRaw = strings.TrimSpace(w.input.Value())
		var data []byte
		var err error
		if w.editing {
			data, err = launchd.BuildEditedPlist(w.orig.PlistPath, w.newJob())
		} else {
			data, err = w.newJob().BuildPlist()
		}
		if err != nil {
			w.errMsg = err.Error()
			return m, nil
		}
		w.preview = string(data)
		w.confirmSel = 0
		w.step = wConfirm

	case wConfirm:
		switch w.confirmSel {
		case 2: // cancel
			m.mode = listView
			m.wiz = nil
			return m, nil
		default:
			load := w.confirmSel == 0
			nj := w.newJob()
			var err error
			verb := "created"
			if w.editing {
				err = launchd.SaveEdited(w.orig, nj, load)
				verb = "updated"
				if load {
					verb = "updated & applied"
				}
			} else {
				err = nj.Create(load)
				if load {
					verb = "created & loaded"
				}
			}
			if err != nil {
				w.errMsg = err.Error()
				return m, nil
			}
			label := nj.Label
			m.mode = listView
			m.wiz = nil
			m.status = okBanner.Render(" ✓ " + verb + ": " + label + " ")
			m = m.rescan()
			for i, j := range m.jobs {
				if j.Label == label {
					m.cursor = i + 1
					break
				}
			}
			return m, nil
		}
	}
	return m, nil
}

func (w *wizard) parseSchedValue(raw string) error {
	switch w.schedKind {
	case launchd.SchedHourly:
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 59 {
			return fmt.Errorf("enter a minute between 0 and 59")
		}
		w.minute = n
	case launchd.SchedDaily:
		mch := regexp.MustCompile(`^(\d{1,2}):(\d{2})$`).FindStringSubmatch(raw)
		if mch == nil {
			return fmt.Errorf("enter a time like 09:30")
		}
		h, _ := strconv.Atoi(mch[1])
		mi, _ := strconv.Atoi(mch[2])
		if h > 23 || mi > 59 {
			return fmt.Errorf("enter a time like 09:30")
		}
		w.hour, w.minute = h, mi
	case launchd.SchedWeekly:
		mch := regexp.MustCompile(`(?i)^(sun|mon|tue|wed|thu|fri|sat) (\d{1,2}):(\d{2})$`).FindStringSubmatch(raw)
		if mch == nil {
			return fmt.Errorf("enter like mon 09:30 (sun/mon/tue/wed/thu/fri/sat)")
		}
		days := map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}
		h, _ := strconv.Atoi(mch[2])
		mi, _ := strconv.Atoi(mch[3])
		if h > 23 || mi > 59 {
			return fmt.Errorf("enter like mon 09:30")
		}
		w.weekday, w.hour, w.minute = days[strings.ToLower(mch[1])], h, mi
	case launchd.SchedInterval:
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return fmt.Errorf("enter minutes (1 or more)")
		}
		w.intervalMin = n
	case launchd.SchedOnce:
		mch := regexp.MustCompile(`^(\d{1,2})-(\d{1,2}) (\d{1,2}):(\d{2})$`).FindStringSubmatch(raw)
		if mch == nil {
			return fmt.Errorf("enter like 01-01 15:00 (MM-DD HH:MM)")
		}
		mo, _ := strconv.Atoi(mch[1])
		d, _ := strconv.Atoi(mch[2])
		h, _ := strconv.Atoi(mch[3])
		mi, _ := strconv.Atoi(mch[4])
		if mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 {
			return fmt.Errorf("enter like 01-01 15:00 (MM-DD HH:MM)")
		}
		w.month, w.day, w.hour, w.minute = mo, d, h, mi
	}
	return nil
}

func (m Model) wizardView() string {
	w := m.wiz
	var b strings.Builder

	title, total, stepNo := "New job", 7, w.step+1
	if w.editing {
		title, total = "Edit job", 6
		if w.step >= wSchedType {
			stepNo = w.step // the label step is skipped
		}
	}
	b.WriteString(titleStyle.Render(title) + "  " + dimStyle.Render(fmt.Sprintf("step %d/%d", stepNo, total)) + "\n\n")

	if w.editing {
		// The existing configuration stays visible on every step, so you
		// always know what you are changing from.
		b.WriteString(sectionStyle.Render("  Current") + "\n")
		cur := func(name, val string) {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  %s ", pad(name+":", 10))) + val + "\n")
		}
		cur("Label", w.orig.Label)
		cur("Script", w.orig.CommandSeed())
		cur("Schedule", w.orig.Schedule)
		if w.orig.StdoutPath != "" {
			cur("Logs", shortenHome(filepath.Dir(w.orig.StdoutPath)))
		}
		if w.orig.EnvPATH != "" {
			cur("PATH", trunc(w.orig.EnvPATH, max(20, m.width-14)))
		} else {
			cur("PATH", dimStyle.Render("— (launchd minimal PATH)"))
		}
		b.WriteString("\n")
	}

	var dn strings.Builder
	done := func(name, val string) {
		dn.WriteString(dimStyle.Render(fmt.Sprintf("  %s %s\n", pad(name+":", 10), val)))
	}
	if w.step > wScript {
		done("Script", w.scriptRaw)
	}
	if !w.editing && w.step > wLabel {
		done("Label", w.label)
	}
	if w.step > wSchedType {
		s := schedOptions[w.schedSel].label
		if w.schedValRaw != "" && w.step > wSchedValue {
			s += " — " + w.schedValRaw
		}
		done("Schedule", s)
	}
	if w.step > wLogDir {
		done("Logs", w.logDirRaw)
	}
	if w.step > wEnvPath {
		v := w.envPathRaw
		if v == "" {
			v = "(none)"
		}
		done("PATH", trunc(v, max(20, m.width-14)))
	}
	if dn.Len() > 0 {
		if w.editing {
			b.WriteString(sectionStyle.Render("  New") + "\n")
		}
		b.WriteString(dn.String())
	}
	b.WriteString("\n")

	switch w.step {
	case wScript:
		b.WriteString(sectionStyle.Render("Which script should this job run?") + "\n")
		b.WriteString(dimStyle.Render("Prepare the script first; the wizard only points launchd at it.") + "\n\n")
		b.WriteString("  " + w.input.View() + "\n")
		b.WriteString(dimStyle.Render("  e.g. ~/scripts/backup.sh") + "\n")
	case wLabel:
		b.WriteString(sectionStyle.Render("Job name (label)") + "\n")
		b.WriteString(dimStyle.Render("Reverse-domain prefix is convention; you normally never think about it.") + "\n\n")
		b.WriteString("  " + w.prefix + w.input.View() + "\n")
		b.WriteString(dimStyle.Render("  letters, digits, . _ - only · e.g. backup-nightly") + "\n")
	case wSchedType:
		b.WriteString(sectionStyle.Render("When should it run?") + "\n")
		if w.editing && w.schedComplex {
			b.WriteString(warnStyle.Render("current schedule is too complex for these presets — picking one replaces it") + "\n")
		}
		b.WriteString("\n")
		for i, opt := range schedOptions {
			if i == w.schedSel {
				b.WriteString(cursorStyle.Render("▸ "+opt.label) + "\n")
			} else {
				b.WriteString("  " + opt.label + "\n")
			}
		}
	case wSchedValue:
		b.WriteString(sectionStyle.Render(schedOptions[w.schedSel].label) + "\n")
		if schedOptions[w.schedSel].kind == launchd.SchedOnce {
			b.WriteString(dimStyle.Render("Runs once, then removes itself. A date already past fires at its next occurrence.") + "\n")
		}
		b.WriteString("\n  " + w.input.View() + "\n")
		b.WriteString(dimStyle.Render("  format: "+schedOptions[w.schedSel].hint) + "\n")
	case wLogDir:
		b.WriteString(sectionStyle.Render("Log directory") + "\n")
		b.WriteString(dimStyle.Render("Written into the plist as StandardOutPath / StandardErrorPath.") + "\n\n")
		b.WriteString("  " + w.input.View() + "\n")
		b.WriteString(dimStyle.Render("  enter = accept this default") + "\n")
	case wEnvPath:
		b.WriteString(sectionStyle.Render("Environment PATH") + "\n")
		b.WriteString(dimStyle.Render("launchd gives jobs a minimal PATH — Homebrew-installed tools are invisible without this.") + "\n\n")
		b.WriteString("  " + w.input.View() + "\n")
		b.WriteString(dimStyle.Render("  tab = insert recommended default · empty = no PATH override") + "\n")
	case wConfirm:
		verb := "written to"
		opts := confirmOptions
		if w.editing {
			verb = "updated:"
			opts = editConfirmOptions
		}
		b.WriteString(sectionStyle.Render("Review — this will be "+verb+" "+shortenHome((launchd.NewJob{Label: w.label}).PlistPath())) + "\n\n")
		b.WriteString(menuBox.Render(strings.TrimRight(w.preview, "\n")) + "\n\n")
		for i, opt := range opts {
			if i == w.confirmSel {
				b.WriteString(cursorStyle.Render("▸ "+opt) + "\n")
			} else {
				b.WriteString("  " + opt + "\n")
			}
		}
	}

	if w.step == wScript || w.step == wLogDir {
		count := fmt.Sprintf(" · %d candidates", len(w.completions))
		if len(w.completions) == 0 {
			count = " · no match"
		}
		b.WriteString("\n" + dimStyle.Render("  in: ") + pathLocStyle.Render(completionDir(w.input.Value())) + dimStyle.Render(count) + "\n")
		if len(w.completions) > 0 {
			b.WriteString(renderCandidates(w.completions, m.width))
		}
	}

	if w.errMsg != "" {
		b.WriteString("\n" + errStyle.Render("✗ "+w.errMsg) + "\n")
	}

	help := "enter next · esc back"
	if w.step == wScript || w.step == wLogDir {
		help = "tab complete path · enter next · esc back"
	}
	if w.step == wEnvPath {
		help = "tab insert default · enter next · esc back"
	}
	if w.step == wSchedType || w.step == wConfirm {
		help = "j/k move · enter select · esc back"
	}
	b.WriteString("\n" + helpStyle.Render(help))
	return b.String()
}
