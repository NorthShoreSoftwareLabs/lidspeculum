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
lidspeculum hold --display       # also keep the screen on, not just the system
lidspeculum run make build       # keep awake only while a command runs
lidspeculum status               # is a hold active? how do I stop it?
lidspeculum stop                 # make the machine sleepable again
```

By default a hold keeps the **system** awake but lets the **screen** sleep — the
lid can shut and the machine stays up while the display powers off. Add
`--display` (on `hold` or `run`) to hold the screen on as well.

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
brew tap NorthShoreSoftwareLabs/tap
brew trust NorthShoreSoftwareLabs/tap
brew install lidspeculum
```

Recent Homebrew requires you to trust a non-official tap once before it will load
the formula (a tap runs code on install, so Homebrew makes you opt in). The
`brew trust` line above is that one-time step; you won't need it again on the same
machine, and `brew upgrade lidspeculum` works normally afterwards. If your
Homebrew is older and doesn't enforce this, the `brew trust` line is a harmless
no-op and `brew install NorthShoreSoftwareLabs/tap/lidspeculum` also works.

### Scoop (Windows)

```
scoop bucket add NorthShoreSoftwareLabs https://github.com/NorthShoreSoftwareLabs/scoop-bucket
scoop install lidspeculum
```

### Without a tap (no trust step)

If you'd rather not trust the tap, install straight from a GitHub Release or
build with Go. Neither needs Homebrew, and neither prompts for trust; the
tradeoff is no `brew upgrade` auto-updates.

**Prebuilt binary.** Grab the archive for your OS/arch from the
[latest release](https://github.com/NorthShoreSoftwareLabs/lidspeculum/releases/latest),
then put it on your `PATH`:

```
# macOS arm64 example; swap in your platform's asset name
curl -sL https://github.com/NorthShoreSoftwareLabs/lidspeculum/releases/latest/download/lidspeculum_darwin_arm64.tar.gz \
  | tar -xz
sudo mv lidspeculum /usr/local/bin/
```

**From source (Go):**

```
go install github.com/NorthShoreSoftwareLabs/lidspeculum@latest
```

`go install` drops the binary in `$(go env GOPATH)/bin` (usually `~/go/bin`);
make sure that directory is on your `PATH`.

**macOS, after installing:** holds prompt for your password (they change a
system power setting via `sudo`). To stop the prompts, run `lidspeculum authorize`
once — see [Skip the password prompt](#skip-the-password-prompt-macos).

## How it works

| OS | Mechanism | Elevation |
| --- | --- | --- |
| macOS | `pmset -a disablesleep 1` while the hold runs | `sudo` (prompts for your password) |
| Linux (systemd) | re-execs under `systemd-inhibit --what=handle-lid-switch:sleep` for the hold's lifetime | none |
| Windows | power-scheme lid-close action set to "Do nothing" (`powercfg`), prior value saved and restored | elevated (Administrator) terminal |

`--display` adds a second, independent lever that keeps the screen on for the
hold's lifetime. It needs no extra elevation beyond what the lid lever already
requires:

| OS | `--display` mechanism |
| --- | --- |
| macOS | runs `caffeinate -d` as a child of the hold (released when the hold ends) |
| Linux (systemd) | adds `idle` to the `systemd-inhibit --what` lock |
| Windows | `SetThreadExecutionState(ES_CONTINUOUS \| ES_DISPLAY_REQUIRED)` for the hold's lifetime |

On macOS and Windows the hold flips a system power setting, so starting one asks
for elevation: `lidspeculum` calls `sudo` for you on macOS and prompts for your
password; on Windows, run it from an elevated terminal. **Linux needs no root at
all** — the hold is kept alive by a `systemd-inhibit` lock that releases
automatically when the hold process exits.

### Skip the password prompt (macOS)

On macOS every hold runs `sudo pmset` to flip the lid-close setting, so you get a
password prompt when a hold **starts** and again when it **ends** (once the sudo
credential cache expires). If you don't want to type your password to start or
stop holds, run this once:

```
lidspeculum authorize      # stop the prompts (asks for your password once)
lidspeculum revoke         # undo it; prompts come back
```

`authorize` installs a drop-in at `/etc/sudoers.d/lidspeculum` granting
passwordless `sudo` for **only** the two exact commands lidspeculum runs:

```
<you> ALL=(root) NOPASSWD: /usr/bin/pmset -a disablesleep 1, /usr/bin/pmset -a disablesleep 0
```

Nothing else gets passwordless `sudo`. The command prints the exact rule and asks
you to confirm before installing, and it validates the file with `visudo` before
it goes live (a malformed `sudoers` file can lock you out of `sudo`, so the check
is mandatory and it rolls back on any problem). This is a macOS-only convenience;
Linux needs no elevation, and Windows uses an elevated terminal rather than a
password.

## Caveats

- Running with the lid shut and no external display restricts airflow. Watch
  temperatures on long, heavy jobs.
- **Linux desktops (GNOME, KDE):** some desktop environments handle the lid
  switch themselves instead of leaving it to logind. The `systemd-inhibit` lock
  covers the logind path; if your machine still sleeps on lid close, your DE is
  handling it. `lidspeculum` prints a warning when it can't confirm the lock was
  taken. The lock is verified to work where logind owns the lid (servers,
  headless boxes, and DEs that defer to logind).
- **`--display` on Linux desktops (GNOME, KDE):** the `idle` inhibitor stops
  logind's idle timeout, but screen blanking on most graphical desktops is driven
  by the compositor, not logind, so the panel may still blank. The flag is
  reliable where logind owns the idle path; on a full desktop, set the screen
  timeout in your DE if the display still sleeps.
- **Windows must be English-language for now.** The prior lid setting is read by
  parsing `powercfg` output, whose labels are localized. On a non-English
  install `lidspeculum` refuses to engage rather than risk changing your setting
  blindly. (It fails safe; it does not corrupt anything.)
- On macOS/Windows the setting is a system flag. If a hold is killed in a way
  that skips cleanup, `lidspeculum stop` restores normal sleep and `status`
  flags the stranded state.
- Windows `stop`: because a SIGTERM can't be delivered to an unrelated process
  the way it can on unix, `stop` restores the saved power setting and clears the
  pidfile directly; the orphaned holder exits on its own deadline.

## Status

The Linux path is validated end to end against a real `systemd-logind`
(inhibitor lock taken, `run` exit-code passthrough, timed self-release,
single-hold enforcement, signal cleanup). macOS and Windows build, vet, and
test on their native CI runners; the privileged macOS/Windows runtime paths
still want a pass on real hardware. Pre-`v0.1.0`.

## License

MIT. See [LICENSE](LICENSE).
