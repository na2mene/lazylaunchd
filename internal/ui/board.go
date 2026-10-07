package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/na2mene/lazylaunchd/internal/launchd"
	"github.com/na2mene/lazylaunchd/internal/power"
)

// BoardData is the experimental board JSON, independent of terminal size.
type BoardData struct {
	Seats      []BoardSeat `json:"seats"`
	SessionLog []BoardRun  `json:"session_log"`
}

type BoardSeat struct {
	Label          string        `json:"label"`
	State          string        `json:"state"`
	RunningSince   *time.Time    `json:"running_since,omitempty"`
	ElapsedSeconds *int          `json:"elapsed_seconds,omitempty"`
	LastRun        *launchd.Run  `json:"last_run,omitempty"`
	History        []launchd.Run `json:"history"`
	NextRun        *time.Time    `json:"next_run,omitempty"`
}

type BoardRun struct {
	Label string    `json:"label"`
	At    time.Time `json:"at"`
	Exit  int       `json:"exit"`
	Dur   int       `json:"dur,omitempty"`
}

// BuildBoard selects worker seats and merges their completed runs, newest first.
// It only reads jobs and history; observation belongs to the existing refresh.
func BuildBoard(jobs []launchd.Job, hist *launchd.History, filter string, now time.Time) BoardData {
	data := BoardData{Seats: []BoardSeat{}, SessionLog: []BoardRun{}}
	f := strings.ToLower(filter)
	for _, j := range jobs {
		if j.Kind != launchd.UserAgent || !j.StateKnown || !j.Loaded ||
			(!j.Timed && !j.KeptAlive) || j.Label == launchd.WatcherLabel ||
			!strings.Contains(strings.ToLower(j.Label), f) {
			continue
		}
		seat := BoardSeat{Label: j.Label, State: "idle", History: []launchd.Run{}}
		if j.Running() {
			seat.State = "working"
			if hist != nil {
				if since, ok := hist.RunningSince(j.Label); ok {
					elapsed := max(0, int(now.Sub(since).Seconds()))
					seat.RunningSince, seat.ElapsedSeconds = &since, &elapsed
				}
			}
		}
		if hist != nil {
			seat.History = append(seat.History, hist.Runs(j.Label)...)
			if len(seat.History) > 0 {
				last := seat.History[len(seat.History)-1]
				seat.LastRun = &last
			}
			for _, r := range seat.History {
				data.SessionLog = append(data.SessionLog, BoardRun{Label: j.Label, At: r.At, Exit: r.Exit, Dur: r.Dur})
			}
		}
		if next, ok := j.NextRun(now); ok {
			seat.NextRun = &next
		}
		data.Seats = append(data.Seats, seat)
	}
	sort.SliceStable(data.SessionLog, func(i, j int) bool {
		return data.SessionLog[i].At.After(data.SessionLog[j].At)
	})
	return data
}

func NewBoard(jobs []launchd.Job, pw power.Status) Model {
	m := New(jobs, pw)
	m.mode = boardView
	return m
}

func boardResult(exit, dur int) string {
	result := "ok"
	if exit != 0 {
		result = fmt.Sprintf("exit %d", exit)
	}
	if dur > 0 {
		result += " " + fmtRunDur(dur)
	}
	return result
}

// boardSeatLine drops optional columns from the right to preserve label space.
func boardSeatLine(seat BoardSeat, hist *launchd.History, width int) string {
	state := dimStyle.Render("○ idle")
	if seat.State == "working" {
		text := "● working"
		if seat.ElapsedSeconds != nil {
			text += " " + (time.Duration(*seat.ElapsedSeconds) * time.Second).String()
		}
		state = runningStyle.Render(text)
	}
	last := dimStyle.Render("last —")
	if seat.LastRun != nil {
		r := seat.LastRun
		text := "last " + r.At.Format("15:04") + " " + boardResult(r.Exit, r.Dur)
		last = okStyle.Render(text)
		if r.Exit != 0 {
			last = errStyle.Render(text)
		}
	}
	columns := []string{state, last, histCell(hist, seat.Label)}
	if seat.NextRun != nil {
		columns = append(columns, dimStyle.Render("next "+seat.NextRun.Format("15:04")))
	}
	labelW := min(48, lipgloss.Width(seat.Label))
	for len(columns) > 0 && labelW+2+lipgloss.Width(strings.Join(columns, "  ")) > width {
		columns = columns[:len(columns)-1]
	}
	if len(columns) == 0 {
		return trunc(seat.Label, width)
	}
	return pad(trunc(seat.Label, labelW), labelW) + "  " + strings.Join(columns, "  ")
}

func renderBoard(jobs []launchd.Job, hist *launchd.History, filter string, width, height int, now time.Time) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	data := BuildBoard(jobs, hist, filter, now)
	working := 0
	for _, seat := range data.Seats {
		if seat.State == "working" {
			working++
		}
	}
	header := titleStyle.Render("lazylaunchd board") + dimStyle.Render(fmt.Sprintf(" · %s · %d jobs · %d working", now.Format("15:04:05"), len(data.Seats), working))
	footer := "b/esc list · / filter · r refresh · q quit"
	if filter != "" {
		footer = "/ " + filter + " · " + footer
	}
	if height == 1 {
		return titleStyle.MaxWidth(width).Render("lazylaunchd board")
	}
	// Tiny terminals use section rules instead of boxes to leave room for seats.
	boxed := width >= 8 && height >= 10
	inner, overhead := width, 4 // header, footer, two section titles
	if boxed {
		inner, overhead = width-4, 8 // plus two borders per box
	}
	clip := dimStyle.UnsetForeground().MaxWidth(inner)
	budget := max(0, height-overhead)
	seatN := min(len(data.Seats), budget)
	if len(data.Seats) == 0 && budget > 0 {
		seatN = 1
	}
	logN := max(0, budget-seatN)
	var seats, logs []string
	seats = append(seats, sectionStyle.MaxWidth(inner).Render("seats"))
	for _, seat := range data.Seats[:min(seatN, len(data.Seats))] {
		seats = append(seats, clip.Render(boardSeatLine(seat, hist, inner)))
	}
	if len(data.Seats) == 0 && seatN > 0 {
		seats = append(seats, dimStyle.MaxWidth(inner).Render("(no matching jobs)"))
	}
	logs = append(logs, sectionStyle.MaxWidth(inner).Render("session log"))
	for _, r := range data.SessionLog[:min(logN, len(data.SessionLog))] {
		result := boardResult(r.Exit, r.Dur)
		labelW := max(1, inner-12-lipgloss.Width(result))
		line := r.At.Format("15:04:05") + "  " + trunc(r.Label, labelW) + "  " + result
		style := okStyle
		if r.Exit != 0 {
			style = errStyle
		}
		logs = append(logs, style.MaxWidth(inner).Render(line))
	}
	if len(data.SessionLog) == 0 && logN > 0 {
		logs = append(logs, dimStyle.MaxWidth(inner).Render("(no runs observed yet)"))
	}
	seatPanel, logPanel := strings.Join(seats, "\n"), strings.Join(logs, "\n")
	if boxed {
		box := menuBox.Padding(0, 1).Width(width - 2)
		seatPanel, logPanel = box.Render(seatPanel), box.Render(logPanel)
	}
	lines := strings.Split(clip.MaxWidth(width).Render(header)+"\n"+seatPanel+"\n"+logPanel, "\n")
	lines = lines[:min(len(lines), height-1)]
	return strings.Join(lines, "\n") + "\n" + helpStyle.MaxWidth(width).Render(footer)
}
