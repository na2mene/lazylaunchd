package launchd

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// readDisabled parses `launchctl print-disabled` for the GUI domain:
// persistent overrides that survive logins and reboots.
func readDisabled() map[string]bool {
	out, err := exec.Command("launchctl", "print-disabled", guiDomain()).Output()
	if err != nil {
		return nil
	}
	m := map[string]bool{}
	re := regexp.MustCompile(`"([^"]+)"\s*=>\s*(true|disabled)`)
	for _, match := range re.FindAllStringSubmatch(string(out), -1) {
		m[match[1]] = true
	}
	return m
}

type state struct {
	pid      int
	lastExit *int
}

// readStates parses `launchctl list` for the current user's GUI domain.
// Output format: "PID\tStatus\tLabel" with "-" for jobs with no process.
func readStates() map[string]state {
	out, err := exec.Command("launchctl", "list").Output()
	if err != nil {
		return map[string]state{}
	}
	m := make(map[string]state)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[2] == "Label" {
			continue
		}
		var st state
		if pid, err := strconv.Atoi(f[0]); err == nil {
			st.pid = pid
		}
		if ec, err := strconv.Atoi(f[1]); err == nil {
			st.lastExit = &ec
		}
		m[f[2]] = st
	}
	return m
}
