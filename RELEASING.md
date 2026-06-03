# Releasing lidspeculum

Releases are automated with [GoReleaser](https://goreleaser.com) via
`.github/workflows/release.yml`. Pushing a `vX.Y.Z` tag builds cross-platform
binaries, publishes a GitHub Release, and updates the Homebrew tap and Scoop
bucket so `brew install` / `scoop install` pick up the new version.

## One-time setup

1. Create two public repos under the `NorthShoreSoftwareLabs` account:
   - `NorthShoreSoftwareLabs/homebrew-tap`
   - `NorthShoreSoftwareLabs/scoop-bucket`

2. Create a Personal Access Token (classic, `repo` scope, or a fine-grained
   token with contents:write on both repos above). On `github.com/settings/tokens`.

3. Add it to this repo as an Actions secret named `HOMEBREW_TAP_TOKEN`:
   ```
   gh secret set HOMEBREW_TAP_TOKEN --repo NorthShoreSoftwareLabs/lidspeculum
   ```
   (The built-in `GITHUB_TOKEN` can publish the Release but cannot push to the
   tap/bucket repos, which is why a PAT is needed.)

## Cutting a release

The version string is injected from the tag via ldflags, so there's nothing to
bump by hand.

1. Tag and push:
   ```
   git tag v0.1.0
   git push origin v0.1.0
   ```
2. The Release workflow runs GoReleaser. When it finishes:
   ```
   brew install NorthShoreSoftwareLabs/tap/lidspeculum
   ```

(Binaries built outside a tag — `go install`, snapshot builds — report `dev`.)

## Local dry run

With GoReleaser installed (`brew install goreleaser`):

```
goreleaser release --snapshot --clean --skip=publish
```

This builds everything into `dist/` without pushing anything, and validates the
config.
