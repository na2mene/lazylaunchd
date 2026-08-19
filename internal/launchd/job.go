package launchd

import "time"

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
	Timed      bool // has StartCalendarInterval or StartInterval
	KeptAlive  bool // has KeepAlive (bool true or conditional dict)

	interval int        // StartInterval seconds
	calendar []calEntry // parsed StartCalendarInterval

	// Runtime state. Daemons run in the system domain, which needs root
	// to inspect, so StateKnown is false for them.
	StateKnown bool
	Loaded     bool
	PID        int
	LastExit   *int
}

// Running reports whether the job currently has a live process.
func (j Job) Running() bool { return j.StateKnown && j.PID > 0 }

// IntervalBased reports whether the job runs on a StartInterval timer,
// whose next fire time launchd does not expose.
func (j Job) IntervalBased() bool { return j.interval > 0 }

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
