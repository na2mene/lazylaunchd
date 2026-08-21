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

// Uninstall removes the watcher job and, with purge, every trace of
// lazylaunchd's own state. User jobs are never touched.
func Uninstall(purge bool) string {
	var b strings.Builder
	_ = launchctl("bootout", guiDomain()+"/"+WatcherLabel)
	_ = launchctl("enable", guiDomain()+"/"+WatcherLabel) // clear any override
	path := NewJob{Label: WatcherLabel}.PlistPath()
	if err := os.Remove(path); err == nil {
		b.WriteString("watcher stopped and removed\n")
	} else if os.IsNotExist(err) {
		b.WriteString("watcher was not installed\n")
	} else {
		b.WriteString("watcher plist: " + err.Error() + "\n")
	}
	if purge {
		home, _ := os.UserHomeDir()
		os.RemoveAll(filepath.Join(home, "Library/Application Support/lazylaunchd"))
		os.RemoveAll(watcherLogDir())
		b.WriteString("history and watcher logs purged\n")
	} else {
		b.WriteString("history kept (add --purge to remove it)\n")
	}
	b.WriteString("your jobs in ~/Library/LaunchAgents are untouched\n")
	b.WriteString("to remove the binary: brew uninstall lazylaunchd\n")
	return b.String()
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

	// Self-update: when brew upgrade (or a rebuild) replaces the binary,
	// exit cleanly — KeepAlive restarts us as the new version.
	exePath, _ := os.Executable()
	var exeStamp time.Time
	if fi, err := os.Stat(exePath); err == nil {
		exeStamp = fi.ModTime()
	}

	hist := LoadHistory()
	notifier := NewNotifier()
	notifiedStale := map[string]time.Time{} // label -> due already notified
	for {
		if fi, err := os.Stat(exePath); err == nil && !exeStamp.IsZero() && !fi.ModTime().Equal(exeStamp) {
			lg.Printf("binary updated — restarting as the new version")
			os.Exit(0)
		}
		if jobs, err := Scan(); err == nil {
			now := time.Now()
			for _, msg := range notifier.Process(hist.Observe(jobs), now) {
				Notify("lazylaunchd", msg)
				lg.Printf("NOTIFY %s", msg)
			}
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
