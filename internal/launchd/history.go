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
}

type Failure struct {
	Label string
	Exit  int
}

type snapshot struct {
	pid     int
	exit    *int
	logSize int64
}

type History struct {
	path string
	runs map[string][]Run
	prev map[string]snapshot
}

const historyCap = 20

func historyPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library/Application Support/lazylaunchd/history.json")
}

func LoadHistory() *History {
	h := &History{path: historyPath(), runs: map[string][]Run{}, prev: map[string]snapshot{}}
	if data, err := os.ReadFile(h.path); err == nil {
		json.Unmarshal(data, &h.runs)
	}
	return h
}

// Runs returns the recorded history for a label, oldest first.
func (h *History) Runs(label string) []Run { return h.runs[label] }

func (h *History) save() {
	os.MkdirAll(filepath.Dir(h.path), 0o755)
	if data, err := json.MarshalIndent(h.runs, "", " "); err == nil {
		os.WriteFile(h.path, data, 0o644)
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
		cur := snapshot{pid: j.PID, exit: j.LastExit, logSize: size}
		p, had := h.prev[j.Label]
		h.prev[j.Label] = cur
		if !had {
			continue // first sight: baseline only
		}

		exit := 0
		if j.LastExit != nil {
			exit = *j.LastExit
		}
		ran := false
		switch {
		case p.pid > 0 && j.PID == 0:
			ran = true // process finished
		case p.pid > 0 && j.PID > 0 && p.pid != j.PID:
			ran = true // keepalive process died and was restarted
		case p.pid == 0 && j.PID == 0 && p.exit != nil && j.LastExit != nil && *p.exit != *j.LastExit:
			ran = true // ran to completion between polls, exit changed
		case p.pid == 0 && j.PID == 0 && size > p.logSize:
			ran = true // ran to completion between polls, same exit, log grew
		}
		if !ran {
			continue
		}

		h.runs[j.Label] = append(h.runs[j.Label], Run{At: time.Now(), Exit: exit})
		if n := len(h.runs[j.Label]); n > historyCap {
			h.runs[j.Label] = h.runs[j.Label][n-historyCap:]
		}
		changed = true
		if exit != 0 {
			fails = append(fails, Failure{Label: j.Label, Exit: exit})
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
