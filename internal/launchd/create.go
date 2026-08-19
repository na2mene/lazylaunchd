package launchd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"howett.net/plist"
)

// Schedule kinds for new jobs.
const (
	SchedHourly = iota
	SchedDaily
	SchedInterval
	SchedKeepAlive
	SchedManual
	SchedOnce // fire at one specific date, then remove itself
)

// shq single-quotes a string for /bin/sh.
func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// NewJob describes a job the wizard wants to create.
type NewJob struct {
	Label      string
	Program    []string
	SchedKind  int
	Minute     int // SchedHourly, SchedDaily, SchedCalendar
	Hour       int // SchedDaily, SchedCalendar
	Month      int // SchedCalendar
	Day        int // SchedCalendar
	IntervalMin int // SchedInterval
	StdoutPath string
	StderrPath string
}

// PlistPath is where the job definition will be written.
func (n NewJob) PlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library/LaunchAgents", n.Label+".plist")
}

// BuildPlist renders the job as launchd plist XML.
func (n NewJob) BuildPlist() ([]byte, error) {
	d := map[string]interface{}{
		"Label":            n.Label,
		"ProgramArguments": n.Program,
	}
	if n.StdoutPath != "" {
		d["StandardOutPath"] = n.StdoutPath
	}
	if n.StderrPath != "" {
		d["StandardErrorPath"] = n.StderrPath
	}
	switch n.SchedKind {
	case SchedHourly:
		d["StartCalendarInterval"] = map[string]interface{}{"Minute": n.Minute}
	case SchedDaily:
		d["StartCalendarInterval"] = map[string]interface{}{"Hour": n.Hour, "Minute": n.Minute}
	case SchedOnce:
		d["StartCalendarInterval"] = map[string]interface{}{
			"Month": n.Month, "Day": n.Day, "Hour": n.Hour, "Minute": n.Minute,
		}
		// launchd has no Year field, so a bare calendar entry repeats every
		// year. Wrap the command so the job removes and unloads itself after
		// the first run, making it truly one-shot. bootout comes last: it
		// terminates this very shell.
		var quoted []string
		for _, a := range n.Program {
			quoted = append(quoted, shq(a))
		}
		script := fmt.Sprintf("%s; /bin/rm -f %s; /bin/launchctl bootout gui/$(id -u)/%s",
			strings.Join(quoted, " "), shq(n.PlistPath()), n.Label)
		d["ProgramArguments"] = []string{"/bin/sh", "-c", script}
	case SchedInterval:
		d["StartInterval"] = n.IntervalMin * 60
	case SchedKeepAlive:
		d["KeepAlive"] = true
		d["RunAtLoad"] = true
	case SchedManual:
		d["RunAtLoad"] = false
	}
	return plist.MarshalIndent(d, plist.XMLFormat, "  ")
}

// Create writes the plist, validates it with plutil, and optionally loads it.
// On validation failure the written file is removed again.
func (n NewJob) Create(load bool) error {
	for _, p := range []string{n.StdoutPath, n.StderrPath} {
		if p != "" {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return fmt.Errorf("log dir: %w", err)
			}
		}
	}

	data, err := n.BuildPlist()
	if err != nil {
		return err
	}
	path := n.PlistPath()
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	if out, err := exec.Command("plutil", "-lint", path).CombinedOutput(); err != nil {
		os.Remove(path)
		return fmt.Errorf("plutil: %s", string(out))
	}
	if load {
		return launchctl("bootstrap", guiDomain(), path)
	}
	return nil
}
