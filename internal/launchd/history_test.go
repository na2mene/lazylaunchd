package launchd

import (
	"testing"
	"time"
)

func TestRunningSinceBaseline(t *testing.T) {
	since := time.Date(2026, 10, 7, 16, 41, 0, 0, time.UTC)
	h := &History{runs: map[string][]Run{}, prev: map[string]snapshot{
		"worker": {pid: 123, pidSince: since},
	}}
	j := Job{Label: "worker", StateKnown: true, Loaded: true, PID: 123}
	h.Baseline([]Job{j})
	if got, ok := h.RunningSince(j.Label); !ok || !got.Equal(since) {
		t.Fatalf("same PID lost its start: %s, %v", got, ok)
	}
	if len(h.Runs(j.Label)) != 0 {
		t.Fatal("baseline recorded a run")
	}
	j.PID = 456
	h.Baseline([]Job{j})
	restarted, ok := h.RunningSince(j.Label)
	if !ok || restarted.Equal(since) {
		t.Fatal("new PID kept old start")
	}
	h.Baseline([]Job{j})
	if got, _ := h.RunningSince(j.Label); !got.Equal(restarted) {
		t.Fatal("repeated baseline reset elapsed time")
	}
	j.PID = 0
	h.Baseline([]Job{j})
	if _, ok := h.RunningSince(j.Label); ok {
		t.Fatal("idle job has a start")
	}
	if _, ok := h.RunningSince("missing"); ok {
		t.Fatal("unknown label has a start")
	}
}
