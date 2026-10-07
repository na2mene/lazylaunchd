package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/na2mene/lazylaunchd/internal/launchd"
)

func boardFixture(t *testing.T) ([]launchd.Job, *launchd.History, time.Time) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "Library/Application Support/lazylaunchd/history.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{
		"worker.alpha":[{"at":"2026-10-07T16:40:00+09:00","exit":0,"dur":23}],
		"worker.beta":[{"at":"2026-10-07T16:39:00+09:00","exit":0},
		               {"at":"2026-10-07T16:41:00+09:00","exit":1,"dur":23}],
		"com.lazylaunchd.watcher":[{"at":"2026-10-07T16:42:00+09:00","exit":1}]
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	jobs := []launchd.Job{
		{Label: "worker.alpha", Kind: launchd.UserAgent, StateKnown: true, Loaded: true, Timed: true, PID: 123},
		{Label: "worker.beta", Kind: launchd.UserAgent, StateKnown: true, Loaded: true, KeptAlive: true},
		{Label: "worker.empty", Kind: launchd.UserAgent, StateKnown: true, Loaded: true, Timed: true},
		{Label: launchd.WatcherLabel, Kind: launchd.UserAgent, StateKnown: true, Loaded: true, KeptAlive: true, PID: 456},
		{Label: "excluded.unloaded", Kind: launchd.UserAgent, StateKnown: true, Timed: true},
		{Label: "excluded.unknown", Kind: launchd.UserAgent, Loaded: true, Timed: true},
		{Label: "excluded.manual", Kind: launchd.UserAgent, StateKnown: true, Loaded: true},
		{Label: "excluded.system", Kind: launchd.GlobalAgent, StateKnown: true, Loaded: true, Timed: true},
		{Label: "excluded.daemon", Kind: launchd.Daemon, StateKnown: true, Loaded: true, KeptAlive: true},
	}
	hist := launchd.LoadHistory()
	hist.Baseline(jobs)
	since, ok := hist.RunningSince("worker.alpha")
	if !ok {
		t.Fatal("running baseline has no start time")
	}
	return jobs, hist, since.Add(151 * time.Second)
}

func TestRenderBoard(t *testing.T) {
	jobs, hist, now := boardFixture(t)
	view := renderBoard(jobs, hist, "", 140, 24, now)
	for _, want := range []string{
		"lazylaunchd board", "3 jobs · 1 working", "worker.alpha", "worker.beta",
		"● working 2m31s", "○ idle", "last 16:40 ok 23s", "last 16:41 exit 1 23s",
		"last —", "····●", "···●✗", "session log", "16:41:00  worker.beta  exit 1 23s",
		"16:40:00  worker.alpha  ok 23s", "16:39:00  worker.beta  ok",
		"b/esc list · / filter · r refresh · q quit",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q in:\n%s", want, view)
		}
	}
	if strings.Contains(view, launchd.WatcherLabel) || strings.Contains(view, "excluded.") {
		t.Fatalf("ineligible jobs are visible:\n%s", view)
	}
	log := strings.Split(view, "session log")[1]
	if strings.Index(log, "16:41:00") > strings.Index(log, "16:40:00") ||
		strings.Index(log, "16:40:00") > strings.Index(log, "16:39:00") {
		t.Fatal("session log is not newest first")
	}
	if boardResult(0, 0) != "ok" || boardResult(1, 0) != "exit 1" {
		t.Fatal("unknown durations should be omitted")
	}
}

func TestBuildBoard(t *testing.T) {
	jobs, hist, now := boardFixture(t)
	data := BuildBoard(jobs, hist, "", now)
	if len(data.Seats) != 3 || len(data.SessionLog) != 3 {
		t.Fatalf("unexpected board data: %+v", data)
	}
	if data.Seats[0].State != "working" || *data.Seats[0].ElapsedSeconds != 151 || data.Seats[0].NextRun != nil {
		t.Fatalf("unexpected running seat: %+v", data.Seats[0])
	}
	for i := 1; i < len(data.SessionLog); i++ {
		if data.SessionLog[i].At.After(data.SessionLog[i-1].At) {
			t.Fatal("session log is not newest first")
		}
	}
	filtered := BuildBoard(jobs, hist, "BETA", now)
	if len(filtered.Seats) != 1 || filtered.Seats[0].Label != "worker.beta" || len(filtered.SessionLog) != 2 {
		t.Fatalf("filter not applied to both arrays: %+v", filtered)
	}
	empty := BuildBoard(jobs, hist, "missing", now)
	encoded, err := json.Marshal(empty)
	if err != nil || string(encoded) != `{"seats":[],"session_log":[]}` {
		t.Fatalf("empty arrays: %s, %v", encoded, err)
	}
	withoutHistory := BuildBoard(jobs, nil, "alpha", now)
	if len(withoutHistory.Seats) != 1 || withoutHistory.Seats[0].State != "working" || withoutHistory.Seats[0].ElapsedSeconds != nil {
		t.Fatalf("missing history should not invent an elapsed time: %+v", withoutHistory)
	}
	// Returned slices must not let consumers mutate the observer's records.
	data.Seats[0].History[0].Exit = 99
	if hist.Runs("worker.alpha")[0].Exit != 0 {
		t.Fatal("BuildBoard aliases stored history")
	}
}

func TestBoardFitsTerminal(t *testing.T) {
	jobs, hist, now := boardFixture(t)
	for _, width := range []int{1, 2, 7, 20, 60, 80, 140} {
		for _, height := range []int{1, 2, 4, 9, 10, 24} {
			view := renderBoard(jobs, hist, "", width, height, now)
			if lipgloss.Width(view) > width || lipgloss.Height(view) > height {
				t.Errorf("%dx%d exceeds terminal: %dx%d\n%s", width, height, lipgloss.Width(view), lipgloss.Height(view), view)
			}
		}
	}
	for i := 0; i < 50; i++ {
		jobs = append(jobs, jobs[0])
	}
	view := renderBoard(jobs, hist, "", 80, 24, now)
	if lipgloss.Height(view) > 24 || !strings.Contains(view, "session log") || !strings.Contains(view, "b/esc list") {
		t.Fatalf("many seats overflow or hide sections/footer:\n%s", view)
	}
	line := boardSeatLine(BoardSeat{Label: "worker.alpha", State: "working"}, hist, 20)
	if !strings.Contains(line, "worker.alpha") {
		t.Fatalf("narrow rows should preserve the label: %s", line)
	}
}

func TestBoardKeys(t *testing.T) {
	m := Model{mode: listView, width: 140, height: 24}
	key := func(s string) {
		t.Helper()
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
		if s == "esc" {
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		} else if s == "enter" {
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		}
		updated, cmd := m.Update(msg)
		if cmd != nil {
			t.Fatalf("unexpected command on %s", s)
		}
		m = updated.(Model)
	}
	for _, back := range []string{"b", "esc", "q"} {
		key("b")
		if m.mode != boardView {
			t.Fatal("b did not open board")
		}
		key(back)
		if m.mode != listView {
			t.Fatalf("%s did not return to list", back)
		}
	}
	key("b")
	key("/")
	key("alpha")
	if !m.filtering || m.filter != "alpha" || !strings.Contains(m.View(), " / alpha▌ ") {
		t.Fatal("board does not use live filter input")
	}
	key("enter")
	if m.filtering || m.filter != "alpha" {
		t.Fatal("enter did not retain filter")
	}
	key("esc")
	if m.mode != listView || m.filter != "alpha" {
		t.Fatal("board escape should return to list with the existing filter")
	}
	key("esc")
	if m.filter != "" {
		t.Fatal("list escape no longer clears filter")
	}
	key("b")
	m.confirm = &confirmState{prompt: "install the background watcher?"}
	if !strings.Contains(m.View(), m.confirm.prompt) {
		t.Fatal("initial watcher offer must remain visible in board view")
	}
}
