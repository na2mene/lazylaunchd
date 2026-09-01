# lazylaunchd

A lazygit-style TUI for macOS `launchd`.

See every launchd job on your Mac — what script it runs, on what schedule, whether it's
actually running — without memorizing `launchctl` incantations or hand-reading plist XML.

```
lazylaunchd v1.0.0  ⚡ AC power · sleep prevented (caffeinate, powerd) — jobs run 24/7, even with the lid closed
  sleep: ✓ runs 24/7 · ~ skipped/paused while asleep · ! stops on battery lid-close

    + New job (n)    Search (/)    Sort: groups (s)    Tools (t)
User Agents (3)
▸ ✓ ● com.example.backup-hourly    every hour at :00      →15:00 (12m)  ··●●●  loaded
  ✓ ● com.example.keepawake        always on (KeepAlive)                ●●●●●  running (PID 512)
  ✓ ● com.example.nightly-sync     daily at 03:30         →03:30 (13h)  ●●✗✗●  loaded
System Agents (1)
  ✓ ○ com.example.updater          every 1h                             ·····  not loaded
System Daemons (1)
  ✓ ◌ com.example.checkin          every 15m                            ·····  system domain (root)

enter info & actions · n new · e edit · f log · / filter · s sort by next run · ↑↓ · q quit
```

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

Notifications are polite: the first failure notifies immediately (with the job's
last log line), a crash-looping KeepAlive job is summarized once per five minutes
instead of every ~10s restart, and a recovery notice closes the loop when it comes
back. Missed-run detection covers interval schedules too (anchored to the last
observed run), and the list header warns when the watcher itself is off. In
practice, the log line attached to the first notification is often enough to
identify the cause without opening a log file.

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
| `enter`   | job info + actions: Run now (& follow) / Enable / Disable / Restart / Edit / Duplicate / Delete / Truncate logs / Log |
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
GUI domain. Disable, Restart, and Delete on a **running** job terminate its process
(SIGTERM, then SIGKILL after ~20s) — the confirmation says so explicitly whenever a
live process is at stake. Restart (shown only for running jobs) is how a KeepAlive
job picks up an edited script. Disable also writes a persistent `launchctl disable` override, so a
disabled job stays off across logins and reboots (plain bootout would resurrect it);
Enable clears the override and bootstraps. System daemons stay read-only — they
belong to root.

## Features

- Run now / enable / disable / restart / delete — every action from the TUI,
  with an explicit confirmation whenever a live process is at stake
- New-job wizard: answer a few questions, get a valid plist, loaded — including
  a PATH step (launchd's minimal PATH hides Homebrew tools; the wizard writes
  `EnvironmentVariables.PATH` so plain scripts just work) and a WorkingDirectory
  step (launchd starts jobs in `/`)
- Duplicate — a new job seeded from an existing one
- Per-job "survives lid close?" indicator, machine-wide verdict in the header
- Follow logs live, tail -f style, with grep and stdout/stderr toggle
- Run history (`··●●✗`), failure & recovery notifications, missed-run detection
- Background watcher (`lazylaunchd setup`) — notifies with the TUI closed
- Oversized-log warning and one-key truncation
- Jobs as Code — `export` / `import` with `~`-portable paths
- `doctor` — one-shot health check for CI and setup validation
- Homebrew tap (`brew install na2mene/tap/lazylaunchd`)

## Stability

v1.0.0 is a stability declaration — the parts you build muscle memory and
scripts on are frozen for all of v1:

- **Keybindings.** The keys in the table above keep their meanings.
- **CLI subcommands.** `setup`, `doctor`, `export`, `import`, `uninstall` and
  `--dump` stay backward-compatible; new subcommands and flags may be added.
- **Export format.** `export` writes schema `"version": 1`, and every v1
  release reads and writes it. An incompatible schema change would mean a new
  schema version and a v2 of lazylaunchd.

Colors, layout, exact wording, and the internal format of `history.json` are
not part of the contract and may change in any release.

## Known limitations

- **Run history is observed, not instrumented.** launchd keeps no run history,
  so lazylaunchd polls (about every 10s under the watcher) and records what it
  sees. Durations (`● ok ~40s`) are approximate — about ±10s — and a run faster
  than one polling interval is recorded without a duration. If you need
  Jenkins-grade per-second timing, instrument the script itself.
- **A handoff can drop one history entry.** Observation belongs to the watcher
  while it runs and to the TUI otherwise; a run that starts *and* finishes
  exactly while they swap can go unrecorded. That's one missing dot in the
  history column — the job itself is unaffected.
- **System daemons are read-only.** `/Library/LaunchDaemons` definitions are
  listed, but their runtime state and control belong to root.

## Name

Named in the spirit of [lazygit](https://github.com/jesseduffield/lazygit) and
[lazydocker](https://github.com/jesseduffield/lazydocker). Not affiliated with them.

## License

MIT
