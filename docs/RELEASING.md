# Releasing BurnBar

BurnBar is distributed as an unsigned Windows executable. Releases do not require
a signing service, certificate, or manual approval.

## Publish a version

Tag the commit you want to release and push it to GitHub:

```powershell
git tag -a v0.3.0 -m "BurnBar v0.3.0"
git push origin main v0.3.0
```

The `build` workflow runs vet, generates the icon, manifest and version
information, then builds `burnbar.exe`. It uploads `burnbar.exe`,
`burnbar.exe.sha256` directly to a GitHub release,
without using Actions artifact storage. A version such as `v0.3.0` creates a normal release; a version with a
suffix such as `v0.3.0-beta.1` creates a prerelease.

Windows may warn about or block an unsigned executable. Release assets include
a SHA-256 checksum so downloads can be checked with `Get-FileHash`.

## Development builds

`development` is the integration branch. Every successful push builds an unsigned
Windows binary and publishes a uniquely tagged GitHub prerelease
`dev-<run_id>-<run_attempt>`. Its assets are never replaced, avoiding mixed binary
and checksum downloads. A prerelease never becomes GitHub's latest stable release.
Development installs select the newest matching published development release;
unrelated beta/RC tags are excluded. If a build fails, the previous published build
remains installable.

## npm installer publishing

The root `package.json` defines the dependency-free `burnbar` CLI. It bundles
`npm/bin/burnbar.cjs` and internal `npm/windows/install.ps1`, not executable binaries or provider data.
The Node CLI uses the bundled script to retain checksum verification, process
handling, rollback, settings/history preservation and shortcut creation. There is
no install/postinstall hook and no runtime dependency download.

After one-time setup, the `npm` job in `build.yml` runs only after Windows build
and installation smoke checks pass:

| Source | Package version | npm dist-tag | Default binary channel |
| --- | --- | --- | --- |
| Push to `development` | `0.0.0-dev.<run_number>.<run_attempt>` | `development` | development |
| Stable `vX.Y.Z` tag on a commit reachable from `main` | `X.Y.Z` | `latest` | stable |
| PR, normal `main` push, manual run, beta/RC tag | Not published | — | — |

Every development **push event** produces one version; pushing several commits
together builds the pushed tip, not a separate package per intermediate commit.
Stable binaries and npm packages are published on version tags, not ordinary main
commits. npm versions are immutable: do not rerun an already-published stable tag
to overwrite it; tag the next version for a new release. Development reruns use a
new run-attempt suffix. The installer package chooses the latest GitHub release
in its channel, rather than pinning its own package version to a binary.

### npm authentication setup (maintainer)

The package name is `burnbar`, owned by npm account `lahiru98s` after its first
successful publication. Repository secret `NPM_TOKEN` supplies a publish-capable
npm token; repository Actions variable `NPM_PUBLISH_ENABLED=true` enables the job.
The first successful development push can create the npm package; a separate
manual bootstrap publication is not required. No credentials are committed or
included in the npm payload. To replace the token, use GitHub's repository secret
UI or `gh secret set NPM_TOKEN` with a protected input, then retain the enabled
variable. The token must permit package creation/publication and satisfy npm's
2FA policy for noninteractive CI publishing.

As an alternative after the package exists, configure npm trusted publishing to
avoid a long-lived publish token:

1. On npmjs.com, open **burnbar → Settings → Trusted publishing**. Configure GitHub
   owner `nooblk-98`, repository `burnbar`, workflow `build.yml`, no environment.
   Allow direct `npm publish`.
2. Remove the repository's `NPM_TOKEN` secret after validating the OIDC setup. The
   job has `id-token: write` and can use OIDC when no token is configured. See
   [npm trusted publishing](https://docs.npmjs.com/trusted-publishers/).

If publishing is intentionally disabled, set `NPM_PUBLISH_ENABLED=false`.
GitHub builds and GitHub-backed npx installation work independently.

Stable releases are published from version tags after merging into `main`.
`npx --yes burnbar@latest install` installs/updates stable; the development channel
remains separate. Stable tags on commits outside main are rejected before publishing
a stable binary or package. Verify both public dist-tags and npx execution after
npm finishes registry processing; a successful publish job alone is not proof
that the installer is available yet.

### Installer verification

The Windows workflow packs the exact npm payload and invokes it through npx to
install and update the current stable executable in a temporary directory without
launching it. It verifies an up-to-date executable is not rewritten and settings
and history fixtures are unchanged. Development pushes additionally install the
published development binary without launching it. Stable version tags additionally
install the newly published stable binary and verify it matches the build checksum. These checks do not validate
live desktop behavior. Local test sources remain excluded from Git.

npx is the supported installation entry point. The old root `install.ps1` and
remote-script bootstrap are removed. The verified Windows helper now lives only
inside the npm package at `npm/windows/install.ps1`; the CLI supplies its channel
and options. GitHub releases contain only the binary and checksum, not a standalone
installer. Preserve `burnbar.exe` and `burnbar.exe.sha256` in both channels.

## Optional winget publishing

The winget job is disabled unless `WINGET_ENABLED` is set to `true`. It only
updates a package that already exists in `microsoft/winget-pkgs`.

1. Submit the first package using [wingetcreate](https://github.com/microsoft/winget-create),
   with installer type **portable**, package identifier `nooblk-98.BurnBar`, and
   command alias `burnbar`.
2. After the package is accepted, fork `microsoft/winget-pkgs` and save a token
   for submitting updates as the repository secret `WINGET_TOKEN`.
3. Set repository variables `WINGET_PACKAGE_ID` to `nooblk-98.BurnBar` and
   `WINGET_ENABLED` to `true`.

Normal release tags then submit winget updates automatically. Prerelease tags
do not trigger winget publishing.
