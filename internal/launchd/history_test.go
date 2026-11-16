package launchd

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestHistoryStaleScheduledJobs(t *testing.T) {
	const label = "com.example.worker"
	due := time.Date(2026, time.November, 16, 10, 33, 0, 0, time.UTC)
	oldRuns := []Run{{At: due.Add(-10 * time.Minute), Exit: 1}}
	lastExit := 1
	schedules := []struct {
		name string
		job  Job
	}{
		{name: "interval", job: Job{interval: 600}},
		{name: "calendar", job: Job{calendar: []calEntry{toEntry(map[string]interface{}{"Hour": 10, "Minute": 33})}}},
	}
	for _, schedule := range schedules {
		t.Run(schedule.name, func(t *testing.T) {
			tests := []struct {
				name      string
				pid       int
				now       time.Time
				since     time.Time
				noHistory bool
				unloaded  bool
				wantStale bool
			}{
				{
					name: "running past grace", pid: 1234,
					now: due.Add(10*time.Minute + time.Second),
				},
				{
					name: "running across multiple intervals", pid: 1234,
					now: due.Add(2 * time.Hour),
				},
				{
					name: "not running past grace", wantStale: true,
					now: due.Add(10*time.Minute + time.Second),
				},
				{
					name: "within grace",
					now:  due.Add(10*time.Minute - time.Nanosecond),
				},
				{
					name: "at grace boundary", wantStale: true,
					now: due.Add(10 * time.Minute),
				},
				{
					name: "due before observation started",
					now:  due.Add(11 * time.Minute), since: due.Add(time.Nanosecond),
				},
				{
					name: "due at observation start", wantStale: true,
					now: due.Add(11 * time.Minute), since: due,
				},
				{
					name: "no recorded runs", noHistory: true,
					now: due.Add(11 * time.Minute), wantStale: schedule.name == "calendar",
				},
				{
					name: "running without recorded runs", noHistory: true, pid: 1234,
					now: due.Add(11 * time.Minute),
				},
				{
					name: "unloaded", unloaded: true,
					now: due.Add(11 * time.Minute),
				},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					j := schedule.job
					j.Label, j.StateKnown, j.Loaded = label, true, !tt.unloaded
					j.PID, j.LastExit = tt.pid, &lastExit
					h := &History{runs: map[string][]Run{}, since: due.Add(-time.Hour)}
					if !tt.since.IsZero() {
						h.since = tt.since
					}
					if !tt.noHistory {
						h.runs[label] = append([]Run(nil), oldRuns...)
					}
					gotDue, stale := h.Stale(j, tt.now)
					if stale != tt.wantStale {
						t.Errorf("Stale() = (%v, %v), want stale %v", gotDue, stale, tt.wantStale)
					}
					if tt.wantStale && !gotDue.Equal(due) {
						t.Errorf("Stale() due = %v, want %v", gotDue, due)
					}
					if !tt.wantStale && !gotDue.IsZero() {
						t.Errorf("Stale() due = %v, want zero time", gotDue)
					}
					wantRuns := oldRuns
					if tt.noHistory {
						wantRuns = nil
					}
					if !reflect.DeepEqual(h.Runs(label), wantRuns) {
						t.Errorf("Stale() changed history: got %v, want %v", h.Runs(label), wantRuns)
					}
				})
			}
		})
	}
}

func TestHistoryStaleCalendarRecordedRun(t *testing.T) {
	due := time.Date(2026, time.November, 16, 10, 33, 0, 0, time.UTC)
	j := Job{
		Label: "com.example.worker", StateKnown: true, Loaded: true,
		calendar: []calEntry{toEntry(map[string]interface{}{"Hour": 10, "Minute": 33})},
	}
	for _, tt := range []struct {
		name      string
		at        time.Time
		wantStale bool
	}{
		{name: "older run", at: due.Add(-time.Minute - time.Nanosecond), wantStale: true},
		{name: "one minute tolerance", at: due.Add(-time.Minute)},
		{name: "at due time", at: due},
		{name: "after due time", at: due.Add(time.Minute)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := &History{runs: map[string][]Run{j.Label: {{At: tt.at, Exit: 1}}}, since: due.Add(-time.Hour)}
			gotDue, stale := h.Stale(j, due.Add(11*time.Minute))
			if stale != tt.wantStale {
				t.Errorf("Stale() = (%v, %v), want stale %v", gotDue, stale, tt.wantStale)
			}
		})
	}
}

func TestHistoryStaleRunningUntilCompletion(t *testing.T) {
	// Use an overdue interval anchor; no run is recorded until the PID exits.
	now := time.Now()
	j := Job{Label: "com.example.worker", StateKnown: true, Loaded: true, interval: 600}
	oldRun := Run{At: now.Add(-time.Hour), Exit: 1}
	h := &History{
		path: filepath.Join(t.TempDir(), "history.json"),
		runs: map[string][]Run{j.Label: {oldRun}}, prev: map[string]snapshot{},
		since: now.Add(-2 * time.Hour),
	}
	h.Observe([]Job{j}) // baseline before the next run starts
	j.PID = 1234
	for range 2 {
		h.Observe([]Job{j})
		if due, stale := h.Stale(j, now); stale {
			t.Errorf("running job reported stale at %v", due)
		}
		if got := h.Runs(j.Label); !reflect.DeepEqual(got, []Run{oldRun}) {
			t.Fatalf("running job changed recorded history: %v", got)
		}
	}
	j.PID = 0
	lastExit := 1
	j.LastExit = &lastExit
	h.Observe([]Job{j})
	runs := h.Runs(j.Label)
	if len(runs) != 2 || runs[1].Exit != lastExit {
		t.Fatalf("completion history = %v, want one additional failed run", runs)
	}
	if due, stale := h.Stale(j, runs[1].At); stale {
		t.Errorf("just-completed job reported stale at %v", due)
	}
	if _, stale := h.Stale(j, runs[1].At.Add(20*time.Minute)); !stale {
		t.Error("next missed interval was not detected after completion")
	}
}
