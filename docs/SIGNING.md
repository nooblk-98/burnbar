# Releasing: signing and winget

Releases are built on GitHub Actions, signed through [SignPath](https://signpath.io) with the
SignPath Foundation certificate (free for open-source projects), published as a GitHub release,
and then submitted to winget. Signed builds run without Smart App Control or SmartScreen blocks.

## 1. One-time: SignPath Foundation

1. Push the repository to GitHub (public) and make at least one CI run pass.
2. Apply at <https://signpath.org/apply>. They review the project; requirements include an
   OSI-approved license (this repo uses MIT), builds from GitHub-hosted runners, and a
   code signing policy on the project page (see the README's "Code signing policy" section).
3. Once accepted, in the SignPath web app:
   - Connect the GitHub repository as a trusted build system (*Trusted build systems â†’ GitHub.com*).
   - Create an **artifact configuration** for the uploaded GitHub artifact, which is a zip
     containing the exe:

     ```xml
     <?xml version="1.0" encoding="utf-8"?>
     <artifact-configuration xmlns="http://signpath.io/artifact-configuration/v1">
       <zip-file>
         <pe-file path="burnbar.exe">
           <authenticode-sign />
         </pe-file>
       </zip-file>
     </artifact-configuration>
     ```

   - Note the slugs of the project, the signing policy (`test-signing` for trial runs,
     `release-signing` for real releases, which needs a manual approval per request) and the
     artifact configuration.
   - Create an API token for a CI user that may submit to those policies.
4. In GitHub, *Settings â†’ Secrets and variables â†’ Actions*:

   | Kind | Name | Value |
   |---|---|---|
   | Secret | `SIGNPATH_API_TOKEN` | the SignPath API token |
   | Variable | `SIGNPATH_ORGANIZATION_ID` | your SignPath organization ID |
   | Variable | `SIGNPATH_PROJECT_SLUG` | project slug |
   | Variable | `SIGNPATH_SIGNING_POLICY_SLUG` | `release-signing` (or `test-signing`) |
   | Variable | `SIGNPATH_ARTIFACT_CONFIGURATION_SLUG` | artifact configuration slug |

## 2. Releasing a version

```powershell
git tag v0.2.0
git push origin v0.2.0
```

The `build` workflow tests and builds, submits the exe to SignPath and waits (up to an hour)
for the signing request to be approved in SignPath, checks the signature, then creates the GitHub
release with `burnbar.exe` and `burnbar.exe.sha256`.

## 3. winget

winget's automation can only update a package that already exists, so the first version is
submitted by hand:

1. After the first signed release exists, install [wingetcreate](https://github.com/microsoft/winget-create)
   (`winget install wingetcreate`) and run:

   ```powershell
   wingetcreate new https://github.com/nooblk-98/burnbar/releases/download/v0.2.0/burnbar.exe
   ```

   Use installer type **portable**, package identifier `nooblk-98.BurnBar`, command alias
   `burnbar`, and let it open the pull request to `microsoft/winget-pkgs`.
2. Wait for that pull request to be merged.
3. For automatic updates on later tags:
   - Fork `microsoft/winget-pkgs` to your account.
   - Create a classic personal access token with the `public_repo` scope and save it as the
     secret `WINGET_TOKEN`.
   - Set variables `WINGET_PACKAGE_ID` = `nooblk-98.BurnBar` and `WINGET_ENABLED` = `true`.

Each new `v*` tag then opens a winget-pkgs pull request for the new version automatically.
