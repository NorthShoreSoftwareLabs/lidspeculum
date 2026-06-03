# lidspeculum

Keep your computer awake when the lid is closed.

Closing the lid triggers a different sleep path than going idle, so the usual
keep-awake tools (`caffeinate`, power assertions, "prevent sleep" toggles) don't
stop it. `lidspeculum` drives the one setting on each OS that actually holds the
lid open, behind a single small command.

```
lidspeculum on             # closing the lid won't sleep the machine
lidspeculum off            # restore normal behavior
lidspeculum status         # show the current setting
lidspeculum run <cmd...>   # turn on, run the command, restore on exit
```

The `run` form is the safe one for a build or long job: it turns the setting on,
runs your command, and restores normal sleep when the command exits, even on
failure or Ctrl-C.

```
lidspeculum run go test ./...
lidspeculum run pnpm build
```

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

| OS | Mechanism |
| --- | --- |
| macOS | `pmset disablesleep` |
| Linux (systemd) | `HandleLidSwitch=ignore` logind drop-in; `systemd-inhibit` for `run` |
| Windows | power scheme lid-close action set to "Do nothing" (`powercfg`) |

Because each OS guards this setting, `on`/`off` need admin rights:

- macOS / Linux: `lidspeculum` calls `sudo` for you and will prompt for your
  password.
- Windows: run it from an elevated (Administrator) terminal.

On Linux, `run` uses `systemd-inhibit` and needs no root at all.

## Caveats

- Running with the lid shut and no external display restricts airflow. Watch
  temperatures on long, heavy jobs.
- macOS and Windows `on`/`off` change a persistent system setting. macOS resets
  it on reboot; `lidspeculum off` restores it anytime. On Windows the previous
  lid action is saved and restored.
- `run` is the recommended form precisely because it cleans up after itself.

## License

MIT. See [LICENSE](LICENSE).
