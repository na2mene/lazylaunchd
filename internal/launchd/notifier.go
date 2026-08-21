package launchd

import (
	"fmt"
	"time"
)

const failCooldown = 5 * time.Minute

// Notifier turns raw run events into a polite notification stream. A
// crash-looping KeepAlive job restarts every ~10s; without this it would
// flood Notification Center. Policy:
//   - first failure: notify immediately, with the log detail
//   - while still failing: one summary per cooldown window
//   - first success after failures: a recovery notice
type Notifier struct {
	failingSince map[string]time.Time
	lastNotice   map[string]time.Time
	failsSince   map[string]int // failures seen since the last notice
}

func NewNotifier() *Notifier {
	return &Notifier{
		failingSince: map[string]time.Time{},
		lastNotice:   map[string]time.Time{},
		failsSince:   map[string]int{},
	}
}

func (n *Notifier) Process(events []RunEvent, now time.Time) []string {
	var out []string
	for _, e := range events {
		if e.Exit != 0 {
			if _, failing := n.failingSince[e.Label]; !failing {
				n.failingSince[e.Label] = now
				n.lastNotice[e.Label] = now
				n.failsSince[e.Label] = 0
				msg := fmt.Sprintf("%s failed (exit %d)", e.Label, e.Exit)
				if e.Detail != "" {
					msg += ": " + e.Detail
				}
				out = append(out, msg)
				continue
			}
			n.failsSince[e.Label]++
			if now.Sub(n.lastNotice[e.Label]) >= failCooldown {
				out = append(out, fmt.Sprintf("%s still failing (%d more failures, exit %d)",
					e.Label, n.failsSince[e.Label], e.Exit))
				n.lastNotice[e.Label] = now
				n.failsSince[e.Label] = 0
			}
			continue
		}
		if since, failing := n.failingSince[e.Label]; failing {
			delete(n.failingSince, e.Label)
			delete(n.lastNotice, e.Label)
			delete(n.failsSince, e.Label)
			out = append(out, fmt.Sprintf("%s recovered (was failing since %s)", e.Label, since.Format("15:04")))
		}
	}
	return out
}
