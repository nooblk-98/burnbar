# Releasing BurnBar

BurnBar ships unsigned binaries. Releases are built from the Rust/Tauri app and
published with a SHA-256 checksum.

## Version tags

Tag a commit reachable from `main` and push the tag:

```bash
git tag -a v0.5.0 -m "BurnBar v0.5.0"
git push origin main v0.5.0
```

The `build` workflow stamps the app version from the tag, builds
`rust/target/release/burnbar.exe`, stages it as `bin/burnbar.exe`, smoke-tests the
desktop binary, and publishes `burnbar.exe` + `burnbar.exe.sha256` to a GitHub
release. Stable tags must point at `main`; tags containing a suffix such as
`-beta.1` publish a prerelease.

The same run also builds `burnbar-linux-x64.AppImage`, `burnbar-linux-x64.deb`,
`burnbar-macos-arm64.tar.gz` and `burnbar-macos-x64.tar.gz` (a zipped,
ad-hoc-signed `BurnBar.app`) in the `bundles` job and attaches them, each with a
`.sha256` file, to the same release.

Windows may warn about or block an unsigned executable. The checksum in the
release notes can be verified with `Get-FileHash burnbar.exe -Algorithm SHA256`.

## Development builds

Every successful push to `development` publishes an immutable prerelease tagged
`dev-<run_id>-<run_attempt>` with the same assets. A development install selects
the newest matching prerelease; the stable latest release is never replaced.

## CI

- `rust` runs formatting, clippy, 62 tests, a headless smoke test and release
  builds for Windows, macOS and Linux on every pull request touching `rust/`.
- `build` builds the Windows executable, checks the npm installer, and publishes
  releases, npm packages and winget manifests.

## npm installer

The root `package.json` defines the dependency-free `burnbar` installer. It
bundles `npm/bin/burnbar.cjs`, `npm/windows/install.ps1` and `npm/unix/` (the
Node installer for macOS and Linux); binaries are downloaded from GitHub at install time. There is no lifecycle hook.

After the Windows build, the bundles and the installer smoke check pass, the `npm` job publishes:

| Source | Package version | npm dist-tag | Binary channel |
| --- | --- | --- | --- |
| Push to `development` | `0.0.0-dev.<run_number>.<run_attempt>` | `development` | development |
| Stable `vX.Y.Z` tag on `main` | `X.Y.Z` | `latest` | stable |
| PR, normal main push, manual run | Not published | — | — |

Enable publishing with the `NPM_PUBLISH_ENABLED` repository variable and an
`NPM_TOKEN` secret or trusted publisher. npm versions are immutable; tag a new
version instead of reusing one.

## winget

With `WINGET_ENABLED=true` and `WINGET_PACKAGE_ID` set, the `winget` job updates
the existing package from the release's `burnbar.exe` asset. See the
[winget-releaser action](https://github.com/vedantmgoyal9/winget-releaser).
