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

// Load bootstraps the job into the current user's GUI domain.
func Load(j Job) error {
	if j.Kind == Daemon {
		return fmt.Errorf("system daemons need root: sudo launchctl bootstrap system %s", j.PlistPath)
	}
	return launchctl("bootstrap", guiDomain(), j.PlistPath)
}

// Unload boots the job out of the current user's GUI domain,
// stopping its process if one is running.
func Unload(j Job) error {
	if j.Kind == Daemon {
		return fmt.Errorf("system daemons need root: sudo launchctl bootout system/%s", j.Label)
	}
	return launchctl("bootout", guiDomain()+"/"+j.Label)
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
