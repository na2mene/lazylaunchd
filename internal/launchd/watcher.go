package launchd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The watcher is lazylaunchd watching launchd jobs while the TUI is closed —
// itself registered as a KeepAlive launchd job, managed with the same
// wizard machinery as every other job.

const WatcherLabel = "com.lazylaunchd.watcher"

const watchInterval = 10 * time.Second

func watcherLogDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library/Logs/lazylaunchd")
}

// WatcherInstalled reports whether the watcher's plist exists.
func WatcherInstalled() bool {
	_, err := os.Stat(NewJob{Label: WatcherLabel}.PlistPath())
	return err == nil
}

// stableExecutable prefers Homebrew's version-independent opt path so the
// watcher plist survives `brew upgrade` (Cellar paths change per version).
func stableExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return "lazylaunchd"
	}
	if i := strings.Index(exe, "/Cellar/lazylaunchd/"); i >= 0 {
		opt := exe[:i] + "/opt/lazylaunchd/bin/lazylaunchd"
		if _, err := os.Stat(opt); err == nil {
			return opt
		}
	}
	return exe
}

// SetupWatcher installs (or updates) and starts the watcher job.
// Idempotent: safe to run again after upgrades — it repoints the binary.
func SetupWatcher() (string, error) {
	logDir := watcherLogDir()
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", err
	}
	nj := NewJob{
		Label:      WatcherLabel,
		Program:    []string{stableExecutable(), "watch"},
		SchedKind:  SchedKeepAlive,
		StdoutPath: filepath.Join(logDir, "watcher-launchd.log"),
		StderrPath: filepath.Join(logDir, "watcher-launchd.log"),
	}
	data, err := nj.BuildPlist()
	if err != nil {
		return "", err
	}
	path := nj.PlistPath()
	_, statErr := os.Stat(path)
	existed := statErr == nil

	_ = launchctl("bootout", guiDomain()+"/"+WatcherLabel) // may not be running
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	_ = launchctl("enable", guiDomain()+"/"+WatcherLabel)
	if err := launchctl("bootstrap", guiDomain(), path); err != nil {
		return "", err
	}

	verb := "installed"
	if existed {
		verb = "updated"
	}
	return fmt.Sprintf("watcher %s and running\n  runs:  %s watch\n  plist: %s\n  log:   %s",
		verb, nj.Program[0], path, filepath.Join(logDir, "watcher.log")), nil
}

// rotatingLog is a size-capped, self-rotating log writer: when the file
// exceeds cap it moves to <path>.1 (overwriting the previous generation),
// so total disk use is bounded at 2×cap forever.
type rotatingLog struct {
	path string
	cap  int64
	f    *os.File
}

func openRotatingLog(path string, capBytes int64) (*rotatingLog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &rotatingLog{path: path, cap: capBytes, f: f}, nil
}

func (l *rotatingLog) Printf(format string, args ...interface{}) {
	if fi, err := l.f.Stat(); err == nil && fi.Size() > l.cap {
		l.f.Close()
		os.Rename(l.path, l.path+".1")
		l.f, _ = os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	}
	fmt.Fprintf(l.f, time.Now().Format("2006-01-02 15:04:05")+" "+format+"\n", args...)
}

// Watch runs the headless observer loop: scan, record runs, notify failures.
func Watch() error {
	lg, err := openRotatingLog(filepath.Join(watcherLogDir(), "watcher.log"), 5*1024*1024)
	if err != nil {
		return err
	}
	lg.Printf("watcher started (pid %d)", os.Getpid())

	hist := LoadHistory()
	notifiedStale := map[string]time.Time{} // label -> due already notified
	for {
		if jobs, err := Scan(); err == nil {
			for _, f := range hist.Observe(jobs) {
				Notify("lazylaunchd", fmt.Sprintf("%s failed (exit %d)", f.Label, f.Exit))
				lg.Printf("FAIL %s exit %d", f.Label, f.Exit)
			}
			now := time.Now()
			for _, j := range jobs {
				if due, stale := hist.Stale(j, now); stale && !notifiedStale[j.Label].Equal(due) {
					notifiedStale[j.Label] = due
					Notify("lazylaunchd", fmt.Sprintf("%s missed its %s run", j.Label, due.Format("15:04")))
					lg.Printf("STALE %s missed %s", j.Label, due.Format("2006-01-02 15:04"))
				}
			}
		}
		time.Sleep(watchInterval)
	}
}
