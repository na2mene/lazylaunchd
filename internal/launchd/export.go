package launchd

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"howett.net/plist"
)

// Jobs as Code: export writes every user agent's full plist dict as JSON
// with $HOME rewritten to "~", so a jobs.json moves between machines and
// user names; import reverses the transform. Unmanaged plist keys survive
// because the whole dict travels, not just the wizard's fields.

type exportDoc struct {
	Version int                               `json:"version"`
	Jobs    map[string]map[string]interface{} `json:"jobs"`
}

// mapValues walks a plist-shaped tree applying f to every leaf.
func mapValues(v interface{}, f func(interface{}) interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, vv := range t {
			t[k] = mapValues(vv, f)
		}
		return t
	case []interface{}:
		for i, vv := range t {
			t[i] = mapValues(vv, f)
		}
		return t
	}
	return f(v)
}

// ExportUserAgents renders ~/Library/LaunchAgents (minus the watcher,
// which `lazylaunchd setup` recreates per machine) as portable JSON.
func ExportUserAgents() ([]byte, int, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, 0, err
	}
	dir := filepath.Join(home, "Library/LaunchAgents")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}

	doc := exportDoc{Version: 1, Jobs: map[string]map[string]interface{}{}}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".plist" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var dict map[string]interface{}
		if _, err := plist.Unmarshal(data, &dict); err != nil {
			continue // unreadable plists are the doctor's business
		}
		label, _ := dict["Label"].(string)
		if label == "" {
			label = strings.TrimSuffix(e.Name(), ".plist")
		}
		if label == WatcherLabel {
			continue
		}
		mapValues(dict, func(v interface{}) interface{} {
			if s, ok := v.(string); ok && (s == home || strings.HasPrefix(s, home+"/")) {
				return "~" + strings.TrimPrefix(s, home)
			}
			return v
		})
		doc.Jobs[label] = dict
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	return data, len(doc.Jobs), err
}

// ImportJobs writes the exported jobs into ~/Library/LaunchAgents.
// Existing labels are skipped — import never overwrites. Jobs are written
// unloaded unless load is true.
func ImportJobs(data []byte, load bool) (string, error) {
	var doc exportDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("not a lazylaunchd export: %w", err)
	}
	if len(doc.Jobs) == 0 {
		return "", fmt.Errorf("export contains no jobs")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	labels := make([]string, 0, len(doc.Jobs))
	for l := range doc.Jobs {
		labels = append(labels, l)
	}
	sort.Strings(labels)

	var b strings.Builder
	imported, missing := 0, 0
	for _, label := range labels {
		dict := doc.Jobs[label]
		mapValues(dict, func(v interface{}) interface{} {
			switch t := v.(type) {
			case string:
				if t == "~" || strings.HasPrefix(t, "~/") {
					return filepath.Join(home, strings.TrimPrefix(t, "~"))
				}
			case float64:
				// JSON numbers arrive as floats; launchd needs integers.
				if t == math.Trunc(t) {
					return int(t)
				}
			}
			return v
		})
		path := filepath.Join(home, "Library/LaunchAgents", label+".plist")
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintf(&b, "  skip     %s (already exists)\n", label)
			continue
		}
		out, err := plist.MarshalIndent(dict, plist.XMLFormat, "  ")
		if err != nil {
			fmt.Fprintf(&b, "  error    %s: %v\n", label, err)
			continue
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			fmt.Fprintf(&b, "  error    %s: %v\n", label, err)
			continue
		}
		if lint, err := exec.Command("plutil", "-lint", path).CombinedOutput(); err != nil {
			os.Remove(path)
			fmt.Fprintf(&b, "  error    %s: %s\n", label, strings.TrimSpace(string(lint)))
			continue
		}
		state := "imported"
		if load {
			_ = launchctl("enable", guiDomain()+"/"+label)
			if err := launchctl("bootstrap", guiDomain(), path); err != nil {
				state = "imported (load failed: " + err.Error() + ")"
			} else {
				state = "imported & loaded"
			}
		}
		fmt.Fprintf(&b, "  %-8s %s\n", state, label)
		imported++

		// The export carries definitions only — the scripts travel
		// separately, so tell the user right away when one is missing.
		var prog []string
		if pa, ok := dict["ProgramArguments"].([]interface{}); ok {
			for _, a := range pa {
				if s, ok := a.(string); ok {
					prog = append(prog, s)
				}
			}
		}
		if target := programTarget(prog); target != "" {
			if _, err := os.Stat(target); err != nil {
				missing++
				fmt.Fprintf(&b, "           ⚠ program not found: %s — place the script there before loading\n", target)
			}
		}
	}
	if !load && imported > 0 {
		fmt.Fprintf(&b, "\n%d job(s) written unloaded — enable them from the TUI, or rerun with --load\n", imported)
	}
	if missing > 0 {
		fmt.Fprintf(&b, "%d job(s) reference scripts that don't exist here yet — `lazylaunchd doctor` re-checks anytime\n", missing)
	}
	return b.String(), nil
}
