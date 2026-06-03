# Releasing lidspeculum

Distribution is three repos:

- **lidspeculum** (this repo) produces a GitHub Release on every `vX.Y.Z` tag.
  `.github/workflows/release.yml` runs GoReleaser to cross-compile the binaries,
  write `checksums.txt`, and attach everything to the Release. It uses only the
  workflow's built-in `GITHUB_TOKEN` — no secret to configure.
- **NorthShoreSoftwareLabs/homebrew-tap** holds `Formula/lidspeculum.rb`
  (`brew install NorthShoreSoftwareLabs/tap/lidspeculum`, macOS + Linux).
- **NorthShoreSoftwareLabs/scoop-bucket** holds `bucket/lidspeculum.json`
  (`scoop install lidspeculum`, Windows).

The two manifests are maintained by hand and point at the Release artifacts.
GoReleaser does not push to them (its `brews` generator is deprecated in favor
of macOS-only casks, which would drop Linux), so there is no cross-repo PAT.

## Cutting a release

The version string is injected from the tag via ldflags; nothing to bump by hand.

1. Tag and push:
   ```
   git tag v0.1.0
   git push origin v0.1.0
   ```
2. Wait for the Release workflow to finish; it creates the GitHub Release with
   the archives and `checksums.txt`.
3. Update the Homebrew formula in `homebrew-tap`. It builds from source, so it
   needs only the source-tarball sha256:
   ```
   curl -sL https://github.com/NorthShoreSoftwareLabs/lidspeculum/archive/refs/tags/v0.1.0.tar.gz | shasum -a 256
   ```
   Put that in `url`/`sha256` and bump the version.
4. Update the Scoop manifest in `scoop-bucket` with the Windows zip hashes from
   the Release `checksums.txt` (`lidspeculum_windows_amd64.zip`,
   `lidspeculum_windows_arm64.zip`) and bump `version`.
5. Verify:
   ```
   brew install NorthShoreSoftwareLabs/tap/lidspeculum
   scoop bucket add NorthShoreSoftwareLabs https://github.com/NorthShoreSoftwareLabs/scoop-bucket && scoop install lidspeculum
   ```

## Local dry run

With GoReleaser installed (`brew install goreleaser`):

```
goreleaser release --snapshot --clean --skip=publish
```

Builds everything into `dist/` without pushing, and validates the config.
