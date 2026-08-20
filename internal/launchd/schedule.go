package launchd

import (
	"fmt"
	"strings"
	"time"
)

func humanizeSchedule(raw rawPlist) string {
	var parts []string
	if raw.StartInterval > 0 {
		parts = append(parts, "every "+formatSeconds(raw.StartInterval))
	}
	if s := calendarSummary(raw.StartCalendarInterval); s != "" {
		parts = append(parts, s)
	}
	if s := keepAliveLabel(raw.KeepAlive); s != "" {
		parts = append(parts, s)
	} else if raw.RunAtLoad && len(parts) == 0 {
		return "at load only"
	} else if raw.RunAtLoad {
		parts = append(parts, "at load")
	}
	if len(parts) == 0 {
		return "on demand"
	}
	return strings.Join(parts, " · ")
}

func keepAliveLabel(v interface{}) string {
	switch t := v.(type) {
	case bool:
		if t {
			return "always on (KeepAlive)"
		}
	case map[string]interface{}:
		return "KeepAlive (conditional)"
	}
	return ""
}

func formatSeconds(sec int) string {
	h, m, s := sec/3600, (sec%3600)/60, sec%60
	out := ""
	if h > 0 {
		out += fmt.Sprintf("%dh", h)
	}
	if m > 0 {
		out += fmt.Sprintf("%dm", m)
	}
	if s > 0 || out == "" {
		out += fmt.Sprintf("%ds", s)
	}
	return out
}

type calEntry struct {
	minute, hour, day, weekday, month int
}

func calendarSummary(v interface{}) string {
	entries := normalizeCalendar(v)
	if len(entries) == 0 {
		return ""
	}

	if len(entries) == 1 {
		e := entries[0]
		switch {
		case e.weekday >= 0:
			return fmt.Sprintf("weekly (%s) at %s", weekdayName(e.weekday), hhmm(e.hour, e.minute))
		case e.month >= 0 && e.day >= 0:
			return fmt.Sprintf("on %02d-%02d at %s", e.month, e.day, hhmm(e.hour, e.minute))
		case e.day >= 0:
			return fmt.Sprintf("monthly (day %d) at %s", e.day, hhmm(e.hour, e.minute))
		case e.hour >= 0:
			return fmt.Sprintf("daily at %s", hhmm(e.hour, e.minute))
		case e.minute >= 0:
			return fmt.Sprintf("every hour at xx:%02d", e.minute)
		default:
			return "1 calendar entry"
		}
	}

	// Common pattern: one entry per hour with the same minute.
	minute, sameMinute := entries[0].minute, true
	hours := map[int]bool{}
	for _, e := range entries {
		if e.minute != minute {
			sameMinute = false
		}
		if e.hour >= 0 {
			hours[e.hour] = true
		}
	}
	if sameMinute && len(hours) == 24 {
		return fmt.Sprintf("every hour at xx:%02d", nonNegative(minute))
	}
	if sameMinute && len(hours) == len(entries) {
		return fmt.Sprintf("%d times/day at xx:%02d", len(entries), nonNegative(minute))
	}
	return fmt.Sprintf("%d calendar entries", len(entries))
}

func normalizeCalendar(v interface{}) []calEntry {
	switch t := v.(type) {
	case map[string]interface{}:
		return []calEntry{toEntry(t)}
	case []interface{}:
		var out []calEntry
		for _, e := range t {
			if m, ok := e.(map[string]interface{}); ok {
				out = append(out, toEntry(m))
			}
		}
		return out
	}
	return nil
}

func toEntry(m map[string]interface{}) calEntry {
	get := func(key string) int {
		v, ok := m[key]
		if !ok {
			return -1
		}
		switch n := v.(type) {
		case uint64:
			return int(n)
		case int64:
			return int(n)
		case int:
			return n
		case float64:
			return int(n)
		}
		return -1
	}
	return calEntry{
		minute:  get("Minute"),
		hour:    get("Hour"),
		day:     get("Day"),
		weekday: get("Weekday"),
		month:   get("Month"),
	}
}

func hhmm(hour, minute int) string {
	return fmt.Sprintf("%02d:%02d", nonNegative(hour), nonNegative(minute))
}

func nonNegative(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// nextCalendar finds the next time matching a StartCalendarInterval entry.
// launchd ANDs the specified components; missing ones are wildcards.
func nextCalendar(e calEntry, now time.Time) time.Time {
	start := now.Add(time.Minute).Truncate(time.Minute)
	for d := 0; d < 400; d++ {
		day := start.AddDate(0, 0, d)
		hFrom, mFrom := 0, 0
		if d == 0 {
			hFrom, mFrom = start.Hour(), start.Minute()
		} else {
			day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
		}
		if e.month >= 0 && int(day.Month()) != e.month {
			continue
		}
		if e.day >= 0 && day.Day() != e.day {
			continue
		}
		if e.weekday >= 0 && int(day.Weekday()) != e.weekday%7 {
			continue
		}
		for h := hFrom; h < 24; h++ {
			if e.hour >= 0 && h != e.hour {
				continue
			}
			from := 0
			if h == hFrom && d == 0 {
				from = mFrom
			}
			if e.minute >= 0 {
				if e.minute >= from {
					return time.Date(day.Year(), day.Month(), day.Day(), h, e.minute, 0, 0, day.Location())
				}
				continue
			}
			return time.Date(day.Year(), day.Month(), day.Day(), h, from, 0, 0, day.Location())
		}
	}
	return time.Time{}
}

// prevCalendar finds the most recent time matching the entry, at or before
// now — the mirror of nextCalendar, used for missed-run detection.
func prevCalendar(e calEntry, now time.Time) time.Time {
	start := now.Truncate(time.Minute)
	for d := 0; d < 400; d++ {
		day := start.AddDate(0, 0, -d)
		hFrom, mFrom := 23, 59
		if d == 0 {
			hFrom, mFrom = start.Hour(), start.Minute()
		} else {
			day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
		}
		if e.month >= 0 && int(day.Month()) != e.month {
			continue
		}
		if e.day >= 0 && day.Day() != e.day {
			continue
		}
		if e.weekday >= 0 && int(day.Weekday()) != e.weekday%7 {
			continue
		}
		for h := hFrom; h >= 0; h-- {
			if e.hour >= 0 && h != e.hour {
				continue
			}
			to := 59
			if h == hFrom && d == 0 {
				to = mFrom
			}
			if e.minute >= 0 {
				if e.minute <= to {
					return time.Date(day.Year(), day.Month(), day.Day(), h, e.minute, 0, 0, day.Location())
				}
				continue
			}
			return time.Date(day.Year(), day.Month(), day.Day(), h, to, 0, 0, day.Location())
		}
	}
	return time.Time{}
}

func weekdayName(wd int) string {
	names := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	if wd >= 0 && wd < len(names) {
		return names[wd]
	}
	if wd == 7 { // launchd allows 7 for Sunday too
		return "Sun"
	}
	return fmt.Sprintf("wd %d", wd)
}
