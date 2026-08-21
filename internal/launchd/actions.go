package launchd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func guiDomain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

func launchctl(args ...string) error {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("launchctl %s: %s", strings.Join(args, " "), msg)
	}
	return nil
}

// Load clears any persistent disable override and bootstraps the job into
// the current user's GUI domain.
func Load(j Job) error {
	if j.Kind == Daemon {
		return fmt.Errorf("system daemons need root: sudo launchctl bootstrap system %s", j.PlistPath)
	}
	_ = launchctl("enable", guiDomain()+"/"+j.Label) // a disabled service refuses to bootstrap
	return launchctl("bootstrap", guiDomain(), j.PlistPath)
}

// Unload stops the job now AND records a persistent disable override —
// bootout alone would resurrect the job at the next login.
func Unload(j Job) error {
	if j.Kind == Daemon {
		return fmt.Errorf("system daemons need root: sudo launchctl bootout system/%s", j.Label)
	}
	if err := launchctl("bootout", guiDomain()+"/"+j.Label); err != nil {
		return err
	}
	return launchctl("disable", guiDomain()+"/"+j.Label)
}

// moveToTrash moves a file into ~/.Trash, deduplicating the name if needed.
// A missing source is not an error.
func moveToTrash(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dest := filepath.Join(home, ".Trash", filepath.Base(path))
	if _, err := os.Stat(dest); err == nil {
		dest = fmt.Sprintf("%s.%d", dest, time.Now().Unix())
	}
	return os.Rename(path, dest)
}

// Delete unloads the job if needed and moves its plist and log files to the
// Trash, so an accidental delete stays recoverable. Log directories are only
// removed when they carry the job's label and are left empty — log paths can
// be shared between jobs.
func Delete(j Job) error {
	if j.Kind != UserAgent {
		return fmt.Errorf("only your own user agents can be deleted (system files need root)")
	}
	if j.Loaded {
		if err := Unload(j); err != nil {
			return err
		}
	}
	if err := moveToTrash(j.PlistPath); err != nil {
		return err
	}
	// Clear any disable override: a stale entry would mysteriously block a
	// future job created under the same label.
	_ = launchctl("enable", guiDomain()+"/"+j.Label)
	logs := []string{j.StdoutPath}
	if j.StderrPath != "" && j.StderrPath != j.StdoutPath {
		logs = append(logs, j.StderrPath)
	}
	var logErr error
	for _, p := range logs {
		if err := moveToTrash(p); err != nil && logErr == nil {
			logErr = err
		}
		if p != "" && filepath.Base(filepath.Dir(p)) == j.Label {
			os.Remove(filepath.Dir(p)) // only succeeds when empty
		}
	}
	if logErr != nil {
		return fmt.Errorf("plist deleted, but logs: %w", logErr)
	}
	return nil
}

// Reload boots the job out and back in so an edited plist takes effect —
// launchd never picks up plist changes on its own.
func Reload(j Job) error {
	if j.Kind == Daemon {
		return fmt.Errorf("system daemons need root: sudo launchctl")
	}
	_ = launchctl("bootout", guiDomain()+"/"+j.Label) // may already be unloaded
	_ = launchctl("enable", guiDomain()+"/"+j.Label)  // applying implies activating
	return launchctl("bootstrap", guiDomain(), j.PlistPath)
}

const logKeepTail = 1024 * 1024

// TruncateLogs shrinks oversized log files to their last megabyte. Safe for
// files launchd is appending to: launchd opens logs O_APPEND, so the job's
// writes continue at the new end after truncation.
func TruncateLogs(j Job) error {
	seen := map[string]bool{}
	for _, p := range []string{j.StdoutPath, j.StderrPath} {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		fi, err := os.Stat(p)
		if err != nil || fi.Size() <= logKeepTail {
			continue
		}
		f, err := os.OpenFile(p, os.O_RDWR, 0)
		if err != nil {
			return err
		}
		tail := make([]byte, logKeepTail)
		if _, err := f.ReadAt(tail, fi.Size()-logKeepTail); err != nil {
			f.Close()
			return err
		}
		if err := f.Truncate(0); err != nil {
			f.Close()
			return err
		}
		header := fmt.Sprintf("[truncated by lazylaunchd at %s — kept the last 1MB]\n", time.Now().Format("2006-01-02 15:04:05"))
		if _, err := f.WriteAt(append([]byte(header), tail...), 0); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	return nil
}

// Restart kills the running process and starts it fresh — the way to make
// a KeepAlive job pick up an edited script.
func Restart(j Job) error {
	if j.Kind == Daemon {
		return fmt.Errorf("system daemons need root: sudo launchctl kickstart -k system/%s", j.Label)
	}
	return launchctl("kickstart", "-k", guiDomain()+"/"+j.Label)
}

// RunNow starts the job immediately, loading it first if needed.
func RunNow(j Job) error {
	if j.Kind == Daemon {
		return fmt.Errorf("system daemons need root: sudo launchctl kickstart system/%s", j.Label)
	}
	if !j.Loaded {
		if err := Load(j); err != nil {
			return err
		}
	}
	return launchctl("kickstart", guiDomain()+"/"+j.Label)
}
