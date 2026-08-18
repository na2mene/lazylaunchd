// Package power reports whether this Mac will keep running scheduled jobs:
// power source and active sleep-prevention assertions. This is the killer
// question for anyone using a Mac as an always-on job runner — "will my
// jobs survive the lid being closed?"
package power

import (
	"os/exec"
	"strings"
)

type Status struct {
	Known          bool
	OnAC           bool
	SleepPrevented bool
	PreventedBy    string
}

func Read() Status {
	st := Status{}

	out, err := exec.Command("pmset", "-g", "batt").Output()
	if err != nil {
		return st
	}
	st.Known = true
	st.OnAC = strings.Contains(string(out), "AC Power")

	if out, err := exec.Command("pmset", "-g").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			l := strings.TrimSpace(line)
			if !strings.HasPrefix(l, "sleep ") || !strings.Contains(l, "sleep prevented by") {
				continue
			}
			st.SleepPrevented = true
			if i := strings.Index(l, "sleep prevented by "); i >= 0 {
				st.PreventedBy = strings.TrimSuffix(l[i+len("sleep prevented by "):], ")")
			}
		}
	}
	return st
}

// Headline is a one-line verdict for the TUI header.
func (s Status) Headline() string {
	switch {
	case !s.Known:
		return "power state unknown"
	case s.OnAC && s.SleepPrevented:
		return "⚡ AC power · sleep prevented (" + s.PreventedBy + ") — jobs run 24/7, even with the lid closed"
	case s.OnAC:
		return "⚡ AC power · no sleep assertion — jobs stop while the Mac sleeps"
	case s.SleepPrevented:
		return "🔋 battery · sleep prevention is INACTIVE on battery — closing the lid stops jobs"
	default:
		return "🔋 battery — closing the lid stops jobs"
	}
}
