# Releasing BurnBar

BurnBar is distributed as an unsigned Windows executable. Releases do not require
a signing service, certificate, or manual approval.

## Publish a version

Tag the commit you want to release and push it to GitHub:

```powershell
git tag -a v0.2.2 -m "BurnBar v0.2.2"
git push origin main v0.2.2
```

The `build` workflow runs vet, generates the icon, manifest and version
information, then builds `burnbar.exe`. It uploads `burnbar.exe`,
`burnbar.exe.sha256`, and a copy of `install.ps1` directly to a GitHub release,
without using Actions artifact storage. A version such as `v0.2.2` creates a normal release; a version with a
suffix such as `v0.2.2-beta.1` creates a prerelease.

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
`npm/bin/burnbar.cjs` and `install.ps1`, not executable binaries or provider data.
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

### One-time npm setup (maintainer)

The package name is `burnbar`. Availability is not a reservation; if somebody else
claims it, choose a controlled scoped name and update the manifest and commands.
No npm credentials are committed or required for GitHub-backed npx installs.

1. Sign in to the npm account that should own `burnbar` using `npm login`.
2. From a reviewed checkout, publish the first installer package under a version
   not already on npm. For example, to bootstrap development without creating a
   misleading stable `latest` tag:

   ```powershell
   npm version 0.0.0-dev.0 --no-git-tag-version --ignore-scripts
   npm publish --access public --tag development
   ```

   This local version edit is for publication only; do not commit the generated
   version change. The workflow assigns versions automatically.
3. On npmjs.com, open **burnbar → Settings → Trusted publishing**. Configure GitHub
   owner `nooblk-98`, repository `burnbar`, workflow `build.yml`, no environment.
   Allow direct `npm publish`. The job requests `id-token: write` and publishes
   using OIDC and provenance, without a stored npm token. See
   [npm trusted publishing](https://docs.npmjs.com/trusted-publishers/).
4. Set GitHub repository Actions variable `NPM_PUBLISH_ENABLED` to `true`:

   ```powershell
   gh variable set NPM_PUBLISH_ENABLED --repo nooblk-98/burnbar --body true
   ```

5. Push a development commit to prove `burnbar@development`. Merge this workflow
   into `main`, then create the next stable version tag to publish `burnbar@latest`.
   Until the first stable npm publication, use the GitHub-backed stable command.

The publish job stays disabled until this setup is complete; it does not silently
pretend npm publication succeeded. GitHub builds and GitHub-backed npx installation
work independently. For a new tag-based stable release, ensure this workflow and
the package files are on the tagged main commit. Stable tags on commits outside
main are rejected before publishing a stable binary or package.

### Installer verification

The Windows workflow packs the exact npm payload and invokes it through npx to
install and update the current stable executable in a temporary directory without
launching it. It verifies an up-to-date executable is not rewritten and settings
and history fixtures are unchanged. Development pushes additionally install the
published development binary without launching it. These checks do not validate
live desktop behavior. Local test sources remain excluded from Git.

The PowerShell-only entry point remains supported for users without Node.js. Its
`-Channel stable` default uses GitHub's latest stable endpoint; `-Channel development`
selects the development prerelease channel. Preserve the asset names `burnbar.exe`
and `burnbar.exe.sha256` in both channels.

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
