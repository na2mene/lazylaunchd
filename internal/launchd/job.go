package launchd

import (
	"fmt"
	"strings"
	"time"
)

// Kind is the launchd domain a job definition lives in.
type Kind int

const (
	UserAgent Kind = iota
	GlobalAgent
	Daemon
)

func (k Kind) String() string {
	switch k {
	case UserAgent:
		return "User Agents"
	case GlobalAgent:
		return "System Agents"
	case Daemon:
		return "System Daemons"
	}
	return "Unknown"
}

// Job is a single launchd job definition plus its runtime state.
type Job struct {
	Label      string
	PlistPath  string
	Kind       Kind
	Program    []string
	Schedule   string
	StdoutPath string
	StderrPath string
	ParseError string
	Timed      bool   // has StartCalendarInterval or StartInterval
	KeptAlive  bool   // has KeepAlive (bool true or conditional dict)
	EnvPATH    string // EnvironmentVariables.PATH, if the plist sets one
	WorkDir    string // WorkingDirectory, if the plist sets one

	interval int        // StartInterval seconds
	calendar []calEntry // parsed StartCalendarInterval

	// Runtime state. Daemons run in the system domain, which needs root
	// to inspect, so StateKnown is false for them.
	StateKnown bool
	Loaded     bool
	Disabled   bool // persistent launchctl disable override
	PID        int
	LastExit   *int
}

// Running reports whether the job currently has a live process.
func (j Job) Running() bool { return j.StateKnown && j.PID > 0 }

// IntervalBased reports whether the job runs on a StartInterval timer,
// whose next fire time launchd does not expose.
func (j Job) IntervalBased() bool { return j.interval > 0 }

// LastDue is the most recent calendar fire time at or before now — the
// backward mirror of NextRun. False for non-calendar or unloaded jobs.
func (j Job) LastDue(now time.Time) (time.Time, bool) {
	if len(j.calendar) == 0 {
		return time.Time{}, false
	}
	if j.StateKnown && !j.Loaded {
		return time.Time{}, false
	}
	var best time.Time
	for _, e := range j.calendar {
		if t := prevCalendar(e, now); !t.IsZero() && t.After(best) {
			best = t
		}
	}
	return best, !best.IsZero()
}

// CommandSeed renders the job's program as a single editable string for
// the edit form. Wizard-generated shapes collapse back to the bare path.
func (j Job) CommandSeed() string {
	if len(j.Program) == 1 {
		return j.Program[0]
	}
	if len(j.Program) == 2 && j.Program[0] == "/bin/sh" {
		return j.Program[1]
	}
	// One-shot wrapper: peel off the self-removal tail and quoting.
	if len(j.Program) == 3 && j.Program[0] == "/bin/sh" && j.Program[1] == "-c" {
		cmd := j.Program[2]
		if i := strings.Index(cmd, "; /bin/rm -f "); i > 0 {
			cmd = cmd[:i]
		}
		return strings.Trim(cmd, "'")
	}
	return strings.Join(j.Program, " ")
}

// EditSeed maps the job's schedule back onto the wizard presets for
// prefilling the edit form. ok is false for schedules the presets can't
// express (weekday rules, mixed calendars, …).
func (j Job) EditSeed() (kind int, value string, ok bool) {
	if j.KeptAlive {
		return SchedKeepAlive, "", true
	}
	if j.interval > 0 {
		return SchedInterval, fmt.Sprintf("%d", (j.interval+59)/60), true
	}
	if len(j.calendar) == 1 {
		e := j.calendar[0]
		switch {
		case e.weekday >= 0 && e.hour >= 0 && e.minute >= 0 && e.day < 0 && e.month < 0:
			return SchedWeekly, fmt.Sprintf("%s %02d:%02d", strings.ToLower(weekdayName(e.weekday)), e.hour, e.minute), true
		case e.month >= 0 && e.day >= 0 && e.weekday < 0:
			return SchedOnce, fmt.Sprintf("%02d-%02d %02d:%02d", e.month, e.day, nonNegative(e.hour), nonNegative(e.minute)), true
		case e.hour >= 0 && e.minute >= 0 && e.day < 0 && e.weekday < 0 && e.month < 0:
			return SchedDaily, fmt.Sprintf("%02d:%02d", e.hour, e.minute), true
		case e.minute >= 0 && e.hour < 0 && e.day < 0 && e.weekday < 0 && e.month < 0:
			return SchedHourly, fmt.Sprintf("%d", e.minute), true
		}
		return 0, "", false
	}
	if len(j.calendar) > 1 {
		minute, same := j.calendar[0].minute, true
		hours := map[int]bool{}
		for _, e := range j.calendar {
			if e.minute != minute {
				same = false
			}
			if e.hour >= 0 {
				hours[e.hour] = true
			}
		}
		if same && len(hours) == 24 {
			return SchedHourly, fmt.Sprintf("%d", nonNegative(minute)), true
		}
		return 0, "", false
	}
	return SchedManual, "", true
}

// NextRun computes the next calendar fire time. It returns false for jobs
// without a calendar schedule, for unloaded agents (they will not fire),
// and for StartInterval jobs (the timer runs from the last fire, which
// launchd does not expose).
func (j Job) NextRun(now time.Time) (time.Time, bool) {
	if len(j.calendar) == 0 {
		return time.Time{}, false
	}
	if j.StateKnown && !j.Loaded {
		return time.Time{}, false
	}
	var best time.Time
	for _, e := range j.calendar {
		if t := nextCalendar(e, now); !t.IsZero() && (best.IsZero() || t.Before(best)) {
			best = t
		}
	}
	return best, !best.IsZero()
}
