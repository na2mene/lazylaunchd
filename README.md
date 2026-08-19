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

Each row also carries its last five observed runs (`··●●✗`, newest right), and a
failed run posts a macOS notification. launchd keeps no run history itself, so
lazylaunchd records what it observes while open
(`~/Library/Application Support/lazylaunchd/history.json`).

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
go install github.com/na2mene/lazylaunchd@latest
```

## Usage

```sh
lazylaunchd          # TUI
lazylaunchd --dump   # plain table, for scripts / grep
```

### Keys

| Key       | Action                                                        |
|-----------|---------------------------------------------------------------|
| `j` / `k` | move                                                          |
| `enter`   | action menu: Run now & follow log / Run now / Enable / Disable / Edit / Delete / Detail / Follow |
| `e`       | edit via form — the New-job wizard prefilled with current values |
| `n`       | new-job wizard (also via the "+ New job" row at the top)      |
| `d`       | job detail (program, schedule, log tail)                      |
| `f`       | follow log, tail -f style (`t` switches stdout/stderr)        |
| `x`       | shortcut: run now (loads first if needed, then kickstarts)    |
| `u`       | shortcut: load / unload toggle (unload asks for confirmation) |
| `/`       | filter jobs by label (live, esc clears)                       |
| `s`       | toggle sort: grouped ⇄ by next run                            |
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
GUI domain. System daemons stay read-only — they belong to root.

## Roadmap

- [x] Run now / load / unload from the TUI
- [x] New-job wizard: answer a few questions, get a valid plist, loaded
- [x] Per-job "survives lid close?" indicator
- [x] Follow logs live
- [x] Run history (●●✗) and failure notifications
- [x] Homebrew tap (`brew install na2mene/tap/lazylaunchd`)

## Name

Named in the spirit of [lazygit](https://github.com/jesseduffield/lazygit) and
[lazydocker](https://github.com/jesseduffield/lazydocker). Not affiliated with them.

## License

MIT
