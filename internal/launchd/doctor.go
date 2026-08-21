package launchd

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/na2mene/lazylaunchd/internal/power"
)

// latestReleaseTag reads the tag from the releases/latest redirect — no
// API, so no rate limits. Only called from doctor (an explicit action);
// the tool never phones home on its own.
func latestReleaseTag() string {
	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get("https://github.com/na2mene/lazylaunchd/releases/latest")
	if err != nil {
		return ""
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if i := strings.LastIndex(loc, "/tag/"); i >= 0 {
		return loc[i+5:]
	}
	return ""
}

// programTarget picks the file a job actually executes: the /bin/sh wrapper
// target when present, the first argument otherwise ("" for sh -c commands).
func programTarget(prog []string) string {
	if len(prog) == 0 {
		return ""
	}
	if prog[0] == "/bin/sh" && len(prog) > 1 {
		if prog[1] == "-c" {
			return ""
		}
		return prog[1]
	}
	return prog[0]
}

// Doctor runs a one-shot health check over every job definition and prints
// a report. It returns hasErrors=true when something is actually broken
// (warnings alone keep the exit code at zero).
func Doctor(currentVersion string) (string, bool) {
	var b strings.Builder
	errs, warns := 0, 0
	bad := func(format string, args ...interface{}) {
		errs++
		fmt.Fprintf(&b, "✗ "+format+"\n", args...)
	}
	warn := func(format string, args ...interface{}) {
		warns++
		fmt.Fprintf(&b, "⚠ "+format+"\n", args...)
	}

	jobs, err := Scan()
	if err != nil {
		return "scan failed: " + err.Error() + "\n", true
	}
	counts := map[Kind]int{}
	for _, j := range jobs {
		counts[j.Kind]++
	}
	fmt.Fprintf(&b, "lazylaunchd doctor — %d jobs (%d user agents, %d system agents, %d daemons)\n\n",
		len(jobs), counts[UserAgent], counts[GlobalAgent], counts[Daemon])

	seen := map[string]bool{}
	for _, j := range jobs {
		seen[j.Label] = true

		if j.ParseError != "" {
			bad("%s: plist does not parse: %s", j.Label, j.ParseError)
			continue
		}
		if target := programTarget(j.Program); target != "" {
			switch {
			case !filepath.IsAbs(target):
				bad("%s: relative program path %q — launchd starts jobs in /, this will never resolve", j.Label, target)
			default:
				if info, err := os.Stat(target); err != nil {
					bad("%s: program not found: %s", j.Label, target)
				} else if target == j.Program[0] && info.Mode()&0o111 == 0 {
					bad("%s: program is not executable: %s", j.Label, target)
				}
			}
		}
		if j.WorkDir != "" {
			if info, err := os.Stat(j.WorkDir); err != nil || !info.IsDir() {
				warn("%s: working directory missing: %s", j.Label, j.WorkDir)
			}
		}
		var logTotal int64
		dedup := map[string]bool{}
		for _, p := range []string{j.StdoutPath, j.StderrPath} {
			if p == "" || dedup[p] {
				continue
			}
			dedup[p] = true
			if fi, err := os.Stat(p); err == nil {
				logTotal += fi.Size()
			}
		}
		if logTotal > 50*1024*1024 {
			warn("%s: logs at %.0fMB — truncate from the TUI menu", j.Label, float64(logTotal)/(1<<20))
		}
	}

	// Disable overrides pointing at nothing: they silently block any future
	// job created under the same label.
	for label := range readDisabled() {
		if !seen[label] && !strings.HasPrefix(label, "com.apple.") {
			warn("stale disable override for %s (no plist) — clear with: launchctl enable %s/%s", label, guiDomain(), label)
		}
	}

	watcherOn := false
	for _, j := range jobs {
		if j.Label == WatcherLabel && j.Running() {
			watcherOn = true
		}
	}
	if watcherOn {
		fmt.Fprintf(&b, "✓ watcher installed & running\n")
	} else if WatcherInstalled() {
		warn("watcher installed but not running — check: launchctl print %s/%s", guiDomain(), WatcherLabel)
	} else {
		warn("watcher not installed — failures go unnoticed while the TUI is closed. run: lazylaunchd setup")
	}

	fmt.Fprintf(&b, "%s\n", power.Read().Headline())

	if currentVersion != "" && currentVersion != "dev" {
		if latest := latestReleaseTag(); latest != "" && latest != currentVersion {
			fmt.Fprintf(&b, "⬆ %s available (you have %s) — brew upgrade lazylaunchd\n", latest, currentVersion)
		}
	}
	fmt.Fprintf(&b, "\n")

	switch {
	case errs == 0 && warns == 0:
		fmt.Fprintf(&b, "all clear ✓\n")
	default:
		fmt.Fprintf(&b, "%d error(s), %d warning(s)\n", errs, warns)
	}
	return b.String(), errs > 0
}
