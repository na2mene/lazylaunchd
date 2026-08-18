package ui

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/na2mene/lazylaunchd/internal/launchd"
	"github.com/na2mene/lazylaunchd/internal/power"
)

// Dump prints jobs as a plain table for non-TTY use (scripts, CI, debugging).
func Dump(w io.Writer, jobs []launchd.Job, pw power.Status) {
	fmt.Fprintln(w, pw.Headline())
	fmt.Fprintln(w)

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "KIND\tLABEL\tSCHEDULE\tSTATE\tPROGRAM")
	for _, j := range jobs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			kindShort(j.Kind),
			j.Label,
			j.Schedule,
			plainState(j),
			trunc(strings.Join(j.Program, " "), 60),
		)
	}
	tw.Flush()
}

func kindShort(k launchd.Kind) string {
	switch k {
	case launchd.UserAgent:
		return "user-agent"
	case launchd.GlobalAgent:
		return "sys-agent"
	case launchd.Daemon:
		return "daemon"
	}
	return "?"
}

func plainState(j launchd.Job) string {
	switch {
	case j.Running():
		return fmt.Sprintf("running(%d)", j.PID)
	case j.StateKnown && j.Loaded && j.LastExit != nil && *j.LastExit != 0:
		return fmt.Sprintf("loaded(exit %d)", *j.LastExit)
	case j.StateKnown && j.Loaded:
		return "loaded"
	case j.StateKnown:
		return "not-loaded"
	default:
		return "root-domain"
	}
}
