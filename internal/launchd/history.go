package launchd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// launchd keeps no run history, so lazylaunchd records what it can observe
// while it is open: poll-to-poll transitions in PID, last exit status, and
// stdout size.

type Run struct {
	At   time.Time `json:"at"`
	Exit int       `json:"exit"`
	// Dur is the observed runtime in seconds (0 = finished between polls,
	// too fast to measure). Accuracy is bounded by the poll interval.
	Dur int `json:"dur,omitempty"`
}

type Failure struct {
	Label  string
	Exit   int
	Detail string // last log line, so the notification answers "why?"
}

// lastLogLine pulls the job's most recent non-empty log line, preferring
// stderr — that's usually where the reason lives.
func lastLogLine(j Job) string {
	for _, p := range []string{j.StderrPath, j.StdoutPath} {
		if p == "" {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		fi, err := f.Stat()
		if err != nil || fi.Size() == 0 {
			f.Close()
			continue
		}
		const win = 4096
		off, size := int64(0), fi.Size()
		if size > win {
			off, size = fi.Size()-win, win
		}
		buf := make([]byte, size)
		f.ReadAt(buf, off)
		f.Close()
		lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			if l := strings.TrimSpace(lines[i]); l != "" {
				if len(l) > 120 {
					l = l[:120] + "…"
				}
				return l
			}
		}
	}
	return ""
}

type snapshot struct {
	pid      int
	exit     *int
	logSize  int64
	pidSince time.Time // when we first saw this PID
}

type History struct {
	path  string
	runs  map[string][]Run
	prev  map[string]snapshot
	since time.Time // when this process started observing continuously
}

const historyCap = 20

func historyPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library/Application Support/lazylaunchd/history.json")
}

func LoadHistory() *History {
	h := &History{path: historyPath(), runs: map[string][]Run{}, prev: map[string]snapshot{}, since: time.Now()}
	if data, err := os.ReadFile(h.path); err == nil {
		json.Unmarshal(data, &h.runs)
	}
	// Test hook: pretend observation started earlier (undocumented).
	if v := os.Getenv("LAZYLAUNCHD_SINCE"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			h.since = t
		}
	}
	return h
}

// Stale reports whether the job missed its most recent due time on our
// watch: due + grace has passed, we were already observing at the due
// time, and no run was recorded at or after it.
func (h *History) Stale(j Job, now time.Time) (time.Time, bool) {
	const grace = 10 * time.Minute
	due, ok := j.LastDue(now)
	if !ok {
		return time.Time{}, false
	}
	if now.Before(due.Add(grace)) {
		return time.Time{}, false // still within grace
	}
	if due.Before(h.since) {
		return time.Time{}, false // we weren't watching then; can't judge
	}
	for _, r := range h.runs[j.Label] {
		if !r.At.Before(due.Add(-time.Minute)) {
			return time.Time{}, false // it did run
		}
	}
	return due, true
}

// Runs returns the recorded history for a label, oldest first.
func (h *History) Runs(label string) []Run { return h.runs[label] }

// save writes atomically (tmp + rename) — the TUI may read the file while
// the watcher process is writing it.
func (h *History) save() {
	os.MkdirAll(filepath.Dir(h.path), 0o755)
	data, err := json.MarshalIndent(h.runs, "", " ")
	if err != nil {
		return
	}
	tmp := h.path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, h.path)
	}
}

// Reload re-reads runs recorded by another process (the watcher).
func (h *History) Reload() {
	data, err := os.ReadFile(h.path)
	if err != nil {
		return
	}
	var runs map[string][]Run
	if json.Unmarshal(data, &runs) == nil {
		h.runs = runs
	}
}

// Baseline refreshes snapshots without recording anything — used while the
// watcher owns observation, so a later takeover doesn't double-record.
func (h *History) Baseline(jobs []Job) {
	for _, j := range jobs {
		if !j.StateKnown {
			continue
		}
		var size int64
		if j.StdoutPath != "" {
			if fi, err := os.Stat(j.StdoutPath); err == nil {
				size = fi.Size()
			}
		}
		h.prev[j.Label] = snapshot{pid: j.PID, exit: j.LastExit, logSize: size}
	}
}

// Observe compares this poll against the previous one, records finished
// runs, and returns newly observed failures.
func (h *History) Observe(jobs []Job) []Failure {
	var fails []Failure
	changed := false
	for _, j := range jobs {
		if !j.StateKnown {
			continue // system domain: no visibility without root
		}
		var size int64
		if j.StdoutPath != "" {
			if fi, err := os.Stat(j.StdoutPath); err == nil {
				size = fi.Size()
			}
		}
		now := time.Now()
		cur := snapshot{pid: j.PID, exit: j.LastExit, logSize: size}
		p, had := h.prev[j.Label]
		if j.PID > 0 {
			if p.pid == j.PID {
				cur.pidSince = p.pidSince // same process, keep its start
			} else {
				cur.pidSince = now
			}
		}
		h.prev[j.Label] = cur
		if !had {
			continue // first sight: baseline only
		}

		exit := 0
		if j.LastExit != nil {
			exit = *j.LastExit
		}
		ran := false
		dur := 0
		switch {
		case p.pid > 0 && j.PID == 0:
			ran = true // process finished
			if !p.pidSince.IsZero() {
				dur = int(now.Sub(p.pidSince).Seconds())
			}
		case p.pid > 0 && j.PID > 0 && p.pid != j.PID:
			ran = true // keepalive process died and was restarted
			if !p.pidSince.IsZero() {
				dur = int(now.Sub(p.pidSince).Seconds())
			}
		case p.pid == 0 && j.PID == 0 && p.exit != nil && j.LastExit != nil && *p.exit != *j.LastExit:
			ran = true // ran to completion between polls, exit changed
		case p.pid == 0 && j.PID == 0 && size > p.logSize:
			ran = true // ran to completion between polls, same exit, log grew
		}
		if !ran {
			continue
		}

		h.runs[j.Label] = append(h.runs[j.Label], Run{At: now, Exit: exit, Dur: dur})
		if n := len(h.runs[j.Label]); n > historyCap {
			h.runs[j.Label] = h.runs[j.Label][n-historyCap:]
		}
		changed = true
		if exit != 0 {
			fails = append(fails, Failure{Label: j.Label, Exit: exit, Detail: lastLogLine(j)})
		}
	}
	if changed {
		h.save()
	}
	return fails
}

// Notify posts a macOS notification, best effort.
func Notify(title, message string) {
	esc := func(s string) string { return strings.ReplaceAll(s, `"`, `\"`) }
	script := fmt.Sprintf(`display notification "%s" with title "%s"`, esc(message), esc(title))
	exec.Command("osascript", "-e", script).Start()
}
