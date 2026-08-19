package launchd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
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
