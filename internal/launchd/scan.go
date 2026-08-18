package launchd

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"howett.net/plist"
)

type rawPlist struct {
	Label                 string      `plist:"Label"`
	Program               string      `plist:"Program"`
	ProgramArguments      []string    `plist:"ProgramArguments"`
	StartInterval         int         `plist:"StartInterval"`
	StartCalendarInterval interface{} `plist:"StartCalendarInterval"`
	RunAtLoad             bool        `plist:"RunAtLoad"`
	KeepAlive             interface{} `plist:"KeepAlive"`
	StandardOutPath       string      `plist:"StandardOutPath"`
	StandardErrorPath     string      `plist:"StandardErrorPath"`
}

// Scan reads every job definition from the standard launchd directories
// and joins it with runtime state from `launchctl list`.
func Scan() ([]Job, error) {
	home, _ := os.UserHomeDir()
	dirs := []struct {
		path string
		kind Kind
	}{
		{filepath.Join(home, "Library/LaunchAgents"), UserAgent},
		{"/Library/LaunchAgents", GlobalAgent},
		{"/Library/LaunchDaemons", Daemon},
	}

	states := readStates()

	var jobs []Job
	for _, d := range dirs {
		entries, err := os.ReadDir(d.path)
		if err != nil {
			continue // directory may not exist; that's fine
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".plist" {
				continue
			}
			path := filepath.Join(d.path, e.Name())
			job := parse(path, d.kind)
			if d.kind != Daemon {
				job.StateKnown = true
				if st, ok := states[job.Label]; ok {
					job.Loaded = true
					job.PID = st.pid
					job.LastExit = st.lastExit
				}
			}
			jobs = append(jobs, job)
		}
	}

	sort.SliceStable(jobs, func(i, j int) bool {
		if jobs[i].Kind != jobs[j].Kind {
			return jobs[i].Kind < jobs[j].Kind
		}
		return jobs[i].Label < jobs[j].Label
	})
	return jobs, nil
}

func parse(path string, kind Kind) Job {
	fallbackLabel := strings.TrimSuffix(filepath.Base(path), ".plist")
	job := Job{Label: fallbackLabel, PlistPath: path, Kind: kind}

	data, err := os.ReadFile(path)
	if err != nil {
		job.ParseError = err.Error()
		job.Schedule = "unreadable"
		return job
	}
	var raw rawPlist
	if _, err := plist.Unmarshal(data, &raw); err != nil {
		job.ParseError = err.Error()
		job.Schedule = "parse error"
		return job
	}

	if raw.Label != "" {
		job.Label = raw.Label
	}
	job.Program = raw.ProgramArguments
	if len(job.Program) == 0 && raw.Program != "" {
		job.Program = []string{raw.Program}
	}
	job.Schedule = humanizeSchedule(raw)
	job.StdoutPath = raw.StandardOutPath
	job.StderrPath = raw.StandardErrorPath
	return job
}
