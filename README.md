# lazylaunchd

A lazygit-style TUI for macOS `launchd`.

See every launchd job on your Mac — what script it runs, on what schedule, whether it's
actually running — without memorizing `launchctl` incantations or hand-reading plist XML.

Built for people who run a Mac (or Mac mini) as an always-on box: AI agents, cron-like
jobs, home servers. It answers the question that matters most for that setup:
**"will my jobs keep running with the lid closed?"** — the header shows the machine-wide
verdict, and every job row carries its own sleep-impact indicator:

- `✓` runs 24/7 (AC power + an active sleep assertion keeps the Mac awake)
- `~` skipped or paused while the Mac sleeps (one catch-up run on wake)
- `!` on battery — closing the lid stops scheduled runs
- `·` not schedule-driven; sleep doesn't matter

Each row also carries its last five observed runs (`··●●✗`, newest right). A failed
run posts a macOS notification, and a scheduled job that silently skips its slot is
flagged (`⚠ missed 17:00`) and notified too. launchd keeps no run history itself, so
lazylaunchd records what it observes (`~/Library/Application Support/lazylaunchd/history.json`)
— run `lazylaunchd setup` once and a background watcher (itself a launchd job,
self-rotating logs) keeps observing and notifying even while the TUI is closed.

Run durations (`● ok ~40s`) are measured by polling, so they are approximate —
about ±10s under the watcher — and runs faster than one poll show no duration
at all. If you need Jenkins-grade per-second timing, instrument the script itself.

```
lazylaunchd  ⚡ AC power · sleep prevented (caffeinate, powerd) — jobs run 24/7, even with the lid closed

User Agents (2)
  ● com.example.agent-hourly     every hour at :00        loaded
  ● com.example.keepawake        always on (KeepAlive)    running (PID 512)
System Agents (2)
  ● com.example.notifier         always on (KeepAlive)    running (PID 813)
  ○ com.example.updater          every 1h                 not loaded
System Daemons (1)
  ◌ com.example.checkin          every 15m                system domain (root)

enter actions (run/enable/disable) · d detail · j/k move · r refresh · q quit
```

## Install

```sh
brew install na2mene/tap/lazylaunchd
lazylaunchd setup   # optional: background watcher (failure notifications while the TUI is closed)
```

Or with Go:

```sh
go install github.com/na2mene/lazylaunchd@latest
```

## Usage

```sh
lazylaunchd                       # the TUI
lazylaunchd setup                 # install/update the background watcher
lazylaunchd doctor                # health check (exit 1 when something is broken)
lazylaunchd export > jobs.json    # all user agents as portable JSON ($HOME → ~)
lazylaunchd import jobs.json      # write them back (skips existing; --load to start)
lazylaunchd uninstall             # remove the watcher (--purge also removes history)
lazylaunchd --dump                # plain table, for scripts / grep
```

`export` carries job definitions only — the scripts they run travel separately
(keep them in a git repo). `import` warns per job when a referenced script
doesn't exist yet, and `doctor` re-checks anytime.

`doctor` catches the classics: programs that don't exist or aren't executable,
relative paths (launchd runs jobs from `/`), missing working directories,
oversized logs, and stale disable overrides that would silently block a future
job with the same label.

### Keys

| Key       | Action                                                        |
|-----------|---------------------------------------------------------------|
| `↑`/`↓` (or `k`/`j`) | move                                               |
| `enter`   | job info + actions: Run now (& follow) / Enable / Disable / Edit / Duplicate / Delete / Truncate logs / Log |
| `e`       | edit via form — the New-job wizard prefilled with current values |
| `n`       | new-job wizard (also via the "+ New job" row at the top)      |
| `d`       | job detail (program, schedule, log tail)                      |
| `f`       | follow log, tail -f style — `j/k` scroll, `/` grep, `t` stdout/stderr |
| `x`       | shortcut: run now (loads first if needed, then kickstarts)    |
| `u`       | shortcut: enable / disable toggle — disable stays off across restarts |
| `/`       | filter jobs by label (live, esc clears)                       |
| `s`       | toggle sort: grouped ⇄ by next run                            |
| `t`       | Tools: export / import / doctor / watcher setup, in the TUI   |
| `r`       | refresh                                                       |
| `esc`/`q` | back / quit                                                   |

Every action is reachable from the `enter` menu — the single-key shortcuts are
optional muscle memory, not required knowledge.

### Recommended alias

```sh
alias lzl='lazylaunchd'
```

## What it reads

- `~/Library/LaunchAgents` — your user agents
- `/Library/LaunchAgents` — system-wide agents
- `/Library/LaunchDaemons` — root daemons (definitions only; runtime state needs root)
- `launchctl list` — PID / last exit status
- `pmset -g` — power source and sleep-prevention assertions

Job control (`x` / `u`) uses `launchctl kickstart / bootstrap / bootout` on your own
GUI domain. Disable also writes a persistent `launchctl disable` override, so a
disabled job stays off across logins and reboots (plain bootout would resurrect it);
Enable clears the override and bootstraps. System daemons stay read-only — they
belong to root.

## Roadmap

- [x] Run now / load / unload from the TUI
- [x] New-job wizard: answer a few questions, get a valid plist, loaded
- [x] PATH step — launchd's minimal PATH hides Homebrew tools ("works in
      the terminal, fails under launchd"); the wizard writes
      `EnvironmentVariables.PATH` so plain scripts just work
- [x] Per-job "survives lid close?" indicator
- [x] Follow logs live
- [x] Run history (●●✗) and failure notifications
- [x] Background watcher (`lazylaunchd setup`) — notifies with the TUI closed
- [x] Missed-run detection (scheduled but never ran)
- [x] Oversized-log warning and one-key truncation
- [x] WorkingDirectory step (launchd starts jobs in `/`)
- [x] Duplicate — new job seeded from an existing one
- [x] Jobs as Code — `export` / `import` with `~`-portable paths
- [x] `doctor` — one-shot health check for CI and setup validation
- [x] Homebrew tap (`brew install na2mene/tap/lazylaunchd`)

## Name

Named in the spirit of [lazygit](https://github.com/jesseduffield/lazygit) and
[lazydocker](https://github.com/jesseduffield/lazydocker). Not affiliated with them.

## License

MIT
