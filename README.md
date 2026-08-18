# lazylaunchd

A lazygit-style TUI for macOS `launchd`.

See every launchd job on your Mac — what script it runs, on what schedule, whether it's
actually running — without memorizing `launchctl` incantations or hand-reading plist XML.

Built for people who run a Mac (or Mac mini) as an always-on box: AI agents, cron-like
jobs, home servers. The header answers the question that matters most for that setup:
**"will my jobs keep running with the lid closed?"** — by combining power source and
sleep-assertion state from `pmset`.

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

j/k move · enter detail · r refresh · q quit
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

| Key       | Action                                  |
|-----------|-----------------------------------------|
| `j` / `k` | move                                    |
| `enter`   | job detail (program, schedule, log tail) |
| `r`       | refresh                                 |
| `esc`/`q` | back / quit                             |

## What it reads

- `~/Library/LaunchAgents` — your user agents
- `/Library/LaunchAgents` — system-wide agents
- `/Library/LaunchDaemons` — root daemons (definitions only; runtime state needs root)
- `launchctl list` — PID / last exit status
- `pmset -g` — power source and sleep-prevention assertions

Read-only in v0: it never modifies, loads, or unloads anything.

## Roadmap

- [ ] Run now / load / unload from the TUI
- [ ] New-job wizard: answer a few questions, get a valid plist, loaded
- [ ] Per-job "survives lid close?" indicator
- [ ] Follow logs live
- [ ] Homebrew tap

## Name

Named in the spirit of [lazygit](https://github.com/jesseduffield/lazygit) and
[lazydocker](https://github.com/jesseduffield/lazydocker). Not affiliated with them.

## License

MIT
