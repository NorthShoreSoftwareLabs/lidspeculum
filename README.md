# lidspeculum

Keep your computer awake with the lid closed.

Closing the lid triggers a different sleep path than going idle ("clamshell
sleep"), so the usual keep-awake tools (`caffeinate`, power assertions, "prevent
sleep" toggles) don't stop it. `lidspeculum` drives the one lever on each OS that
actually holds the lid open, behind an opinionated, resident model: you start a
hold and it stays in effect until you stop it.

```
lidspeculum hold                 # keep awake until you press Ctrl-C
lidspeculum hold --for 2h        # keep awake for a while, then auto-release
lidspeculum hold --until 17:00   # keep awake until a wall-clock time today
lidspeculum run make build       # keep awake only while a command runs
lidspeculum status               # is a hold active? how do I stop it?
lidspeculum stop                 # make the machine sleepable again
```

A hold is **resident**: the process stays running and keeps the machine awake
until you stop it (Ctrl-C, `lidspeculum stop`, or a `--for`/`--until` deadline).
`stop` is the off switch and works from any terminal.

The `run` form is the safe one for a build or long job. It holds the machine
awake, runs your command, and releases on the command's exit, on failure, or on
Ctrl-C. It exits with the wrapped command's exit code. A leading `--` is
optional and only needed to separate lidspeculum's flags from the command:

```
lidspeculum run go test ./...
lidspeculum run -- rsync -q src/ dst/
```

## One hold at a time

There is at most one active hold (or `run`) at a time, tracked with a small
pidfile in your per-user state directory. Starting a second hold while one is
already running is refused with a clear message; use `lidspeculum stop` to end
the current one first. If a previous hold crashed without cleaning up,
`lidspeculum stop` clears the leftover override, and `status` reports the
stranded state.

## Install

### Homebrew (macOS, Linux)

```
brew install NorthShoreSoftwareLabs/tap/lidspeculum
```

### Scoop (Windows)

```
scoop bucket add NorthShoreSoftwareLabs https://github.com/NorthShoreSoftwareLabs/scoop-bucket
scoop install lidspeculum
```

### From source

```
go install github.com/NorthShoreSoftwareLabs/lidspeculum@latest
```

## How it works

| OS | Mechanism | Elevation |
| --- | --- | --- |
| macOS | `pmset -a disablesleep 1` while the hold runs | `sudo` (prompts for your password) |
| Linux (systemd) | re-execs under `systemd-inhibit --what=handle-lid-switch:sleep` for the hold's lifetime | none |
| Windows | power-scheme lid-close action set to "Do nothing" (`powercfg`), prior value saved and restored | elevated (Administrator) terminal |

On macOS and Windows the hold flips a system power setting, so starting one asks
for elevation: `lidspeculum` calls `sudo` for you on macOS and prompts for your
password; on Windows, run it from an elevated terminal. **Linux needs no root at
all** — the hold is kept alive by a `systemd-inhibit` lock that releases
automatically when the hold process exits.

## Caveats

- Running with the lid shut and no external display restricts airflow. Watch
  temperatures on long, heavy jobs.
- On macOS/Windows the setting is a system flag. If a hold is killed in a way
  that skips cleanup, `lidspeculum stop` restores normal sleep and `status`
  flags the stranded state.
- Windows `stop`: because a SIGTERM can't be delivered to an unrelated process
  the way it can on unix, `stop` restores the saved power setting and clears the
  pidfile directly; the orphaned holder exits on its own deadline.

## License

MIT. See [LICENSE](LICENSE).
