# Releasing BurnBar

BurnBar is distributed as an unsigned Windows executable. Releases do not require
a signing service, certificate, or manual approval.

## Publish a version

Tag the commit you want to release and push it to GitHub:

```powershell
git tag -a v0.2.2 -m "BurnBar v0.2.2"
git push origin main v0.2.2
```

The `build` workflow runs vet and tests, generates the icon, manifest and version
information, then builds `burnbar.exe`. It uploads `burnbar.exe`,
`burnbar.exe.sha256`, and a copy of `install.ps1` directly to a GitHub release,
without using Actions artifact storage. A version such as `v0.2.2` creates a normal release; a version with a
suffix such as `v0.2.2-beta.1` creates a prerelease.

Windows may warn about or block an unsigned executable. Release assets include
a SHA-256 checksum so downloads can be checked with `Get-FileHash`.

The root `install.ps1` script uses GitHub's latest stable release endpoint and
requires both asset names above. Keep those names stable so the documented
PowerShell install/update command continues to work. The bootstrap command reads
the installer from `main`; it installs release binaries rather than development
builds. Validate installer changes with `powershell -NoProfile -File tests/install.Tests.ps1`.

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
