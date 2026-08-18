package launchd

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

	// Runtime state. Daemons run in the system domain, which needs root
	// to inspect, so StateKnown is false for them.
	StateKnown bool
	Loaded     bool
	PID        int
	LastExit   *int
}

// Running reports whether the job currently has a live process.
func (j Job) Running() bool { return j.StateKnown && j.PID > 0 }
