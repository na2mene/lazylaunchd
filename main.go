package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/na2mene/lazylaunchd/internal/launchd"
	"github.com/na2mene/lazylaunchd/internal/power"
	"github.com/na2mene/lazylaunchd/internal/ui"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "watch":
			// Headless observer: records run history and notifies failures
			// while the TUI is closed. Registered as a job by `setup`.
			if err := launchd.Watch(); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		case "setup":
			msg, err := launchd.SetupWatcher()
			if err != nil {
				fmt.Fprintln(os.Stderr, "setup failed:", err)
				os.Exit(1)
			}
			fmt.Println(msg)
			return
		}
	}

	dump := flag.Bool("dump", false, "print jobs as a plain table and exit (no TUI)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("lazylaunchd", version)
		return
	}

	jobs, err := launchd.Scan()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	if *dump {
		ui.Dump(os.Stdout, jobs, power.Read())
		return
	}

	p := tea.NewProgram(ui.New(jobs, power.Read()), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
