#requires -Version 5.1
# Self-contained installer regression checks. All downloads, processes, and COM
# shortcuts are mocked; file operations use a private directory under .build.
$ErrorActionPreference = 'Stop'
$repositoryRoot = Split-Path $PSScriptRoot -Parent
$installerPath = Join-Path $repositoryRoot 'install.ps1'
$buildRoot = [IO.Path]::GetFullPath((Join-Path $repositoryRoot '.build'))
$testRoot = Join-Path $buildRoot ('installer-tests-' + [Guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($testRoot) | Out-Null
$previousAppData = $env:APPDATA
$previousLocalAppData = $env:LOCALAPPDATA

function Assert-InstallTest {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Invoke-RestMethod {
    [CmdletBinding()]
    param([string]$Uri, [hashtable]$Headers, [int]$TimeoutSec)
    Assert-InstallTest ($Uri -eq 'https://api.github.com/repos/nooblk-98/burnbar/releases/latest') 'Unexpected metadata endpoint'
    $global:BurnBarInstallerTestState.Release
}

function Invoke-WebRequest {
    [CmdletBinding()]
    param([string]$Uri, [hashtable]$Headers, [string]$OutFile, [int]$TimeoutSec, [switch]$UseBasicParsing)
    $state = $global:BurnBarInstallerTestState
    $state.Downloads++
    if ($state.FailDownload) { throw 'Simulated failed download' }
    if ($Uri.EndsWith('.sha256')) {
        $content = $state.HashText
    } elseif ($Uri.EndsWith('/burnbar.exe')) {
        $content = $state.NewContent
        if ($state.Corrupt) { $content = 'corrupted executable' }
    } else { throw 'Unexpected asset URL' }
    [IO.File]::WriteAllText($OutFile, $content, [Text.Encoding]::ASCII)
}

function Get-Process {
    [CmdletBinding()]
    param([string]$Name)
    $global:BurnBarInstallerTestState.Processes
}

function Stop-Process {
    [CmdletBinding()]
    param([int]$Id, [switch]$Force)
    $global:BurnBarInstallerTestState.Stopped += $Id
}

function Start-Process {
    [CmdletBinding()]
    param([string]$FilePath, [string]$WorkingDirectory, [string]$WindowStyle, [switch]$PassThru)
    $global:BurnBarInstallerTestState.Started += $FilePath
    [pscustomobject]@{ HasExited = $false }
}

function New-Object {
    [CmdletBinding()]
    param([string]$ComObject)
    Assert-InstallTest ($ComObject -eq 'WScript.Shell') 'Unexpected COM object'
    $shell = [pscustomobject]@{}
    $shell | Add-Member ScriptMethod CreateShortcut {
        param($Path)
        $shortcut = [pscustomobject]@{
            Path = $Path; TargetPath = ''; WorkingDirectory = ''; IconLocation = ''; Description = ''
        }
        $shortcut | Add-Member ScriptMethod Save {
            $global:BurnBarInstallerTestState.ShortcutTarget = $this.TargetPath
            [IO.File]::WriteAllText($this.Path, $this.TargetPath)
        }
        $shortcut
    }
    $shell
}

function New-TestProcess {
    param([string]$Path, [int]$Id)
    $process = [pscustomobject]@{
        Path = $Path; Id = $Id; SessionId = [Diagnostics.Process]::GetCurrentProcess().SessionId; HasExited = $false
    }
    $process | Add-Member ScriptMethod CloseMainWindow { $false }
    $process | Add-Member ScriptMethod WaitForExit { param($Timeout) $true }
    $process
}

try {
    $parseTokens = $null
    $parseErrors = $null
    $null = [Management.Automation.Language.Parser]::ParseFile($installerPath, [ref]$parseTokens, [ref]$parseErrors)
    Assert-InstallTest ($parseErrors.Count -eq 0) 'Installer syntax is invalid'

    foreach ($scenario in @('fresh', 'update', 'already-current', 'corrupt', 'download-failure', 'missing-checksum', 'malformed-checksum', 'digest-mismatch', 'wrong-origin', 'locked-file', 'what-if', 'launch', 'bootstrap')) {
        $caseRoot = Join-Path $testRoot $scenario
        $env:LOCALAPPDATA = Join-Path $caseRoot 'Local'
        $env:APPDATA = Join-Path $caseRoot 'Roaming'
        $installDir = Join-Path $caseRoot 'Program Folder'
        if ($scenario -in @('fresh', 'bootstrap')) { $installDir = Join-Path $env:LOCALAPPDATA 'Programs\BurnBar' }
        [IO.Directory]::CreateDirectory($installDir) | Out-Null
        $target = Join-Path $installDir 'burnbar.exe'
        $settings = Join-Path $env:APPDATA 'BurnBar\config.json'
        [IO.Directory]::CreateDirectory((Split-Path $settings -Parent)) | Out-Null
        [IO.File]::WriteAllText($settings, '{"startWithWindows":false,"providers":"claude"}')
        $newContent = 'verified new executable fixture'
        $hashFixture = Join-Path $caseRoot 'fixture.exe'
        [IO.File]::WriteAllText($hashFixture, $newContent, [Text.Encoding]::ASCII)
        $hash = (Get-FileHash -LiteralPath $hashFixture -Algorithm SHA256).Hash.ToLowerInvariant()
        $assetBase = 'https://github.com/nooblk-98/burnbar/releases/download/v9.9.9/'
        $global:BurnBarInstallerTestState = [pscustomobject]@{
            Release = [pscustomobject]@{
                tag_name = 'v9.9.9'; draft = $false; prerelease = $false
                assets = @(
                    [pscustomobject]@{ name = 'burnbar.exe'; browser_download_url = ($assetBase + 'burnbar.exe'); digest = ('sha256:' + $hash) },
                    [pscustomobject]@{ name = 'burnbar.exe.sha256'; browser_download_url = ($assetBase + 'burnbar.exe.sha256') }
                )
            }
            NewContent = $newContent; HashText = "$hash  burnbar.exe`n"
            Corrupt = ($scenario -eq 'corrupt'); FailDownload = ($scenario -eq 'download-failure')
            Downloads = 0; Processes = @(); Stopped = @(); Started = @(); ShortcutTarget = ''
        }
        $state = $global:BurnBarInstallerTestState
        $oldContent = 'existing working executable fixture'
        if ($scenario -ne 'fresh') {
            if ($scenario -eq 'already-current') { $oldContent = $newContent }
            [IO.File]::WriteAllText($target, $oldContent, [Text.Encoding]::ASCII)
            [IO.File]::SetLastWriteTimeUtc($target, [datetime]'2020-01-01T00:00:00Z')
            $state.Processes = @(New-TestProcess -Path $target -Id 2147483000)
        }
        $oldStamp = $null
        if (Test-Path -LiteralPath $target) { $oldStamp = [IO.File]::GetLastWriteTimeUtc($target) }
        if ($scenario -eq 'missing-checksum') { $state.Release.assets = @($state.Release.assets[0]) }
        if ($scenario -eq 'malformed-checksum') { $state.HashText = 'not a checksum' }
        if ($scenario -eq 'digest-mismatch') { $state.Release.assets[0].digest = 'sha256:' + ('0' * 64) }
        if ($scenario -eq 'wrong-origin') { $state.Release.assets[0].browser_download_url = 'https://example.com/burnbar.exe' }
        $expectedFailure = $scenario -in @('corrupt', 'download-failure', 'missing-checksum', 'malformed-checksum', 'digest-mismatch', 'wrong-origin', 'locked-file')
        $failed = $false
        $lockedStream = $null
        if ($scenario -eq 'locked-file') {
            $lockedStream = [IO.File]::Open($target, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
        }
        try {
            if ($scenario -eq 'fresh') {
                & $installerPath -NoStart
            } elseif ($scenario -eq 'what-if') {
                & $installerPath -InstallDir $installDir -WhatIf
            } elseif ($scenario -eq 'launch') {
                & $installerPath -InstallDir $installDir
            } elseif ($scenario -eq 'bootstrap') {
                & ([scriptblock]::Create((Get-Content -LiteralPath $installerPath -Raw)))
            } else {
                & $installerPath -InstallDir $installDir -NoStart
            }
        } catch {
            $failed = $true
            if (-not $expectedFailure) { throw }
        } finally {
            if ($lockedStream) { $lockedStream.Dispose() }
        }
        Assert-InstallTest ($failed -eq $expectedFailure) "Wrong failure result in $scenario"
        Assert-InstallTest ((Get-Content -LiteralPath $settings -Raw) -eq '{"startWithWindows":false,"providers":"claude"}') "Settings changed in $scenario"
        if ($expectedFailure -or $scenario -eq 'what-if') {
            Assert-InstallTest ((Get-Content -LiteralPath $target -Raw) -eq $oldContent) "Existing binary changed in $scenario"
            if ($scenario -ne 'locked-file') {
                Assert-InstallTest ($state.Stopped.Count -eq 0 -and $state.Started.Count -eq 0) "A process was changed before verification in $scenario"
            }
        } else {
            Assert-InstallTest ((Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash.ToLowerInvariant() -eq $hash) "Installed binary mismatch in $scenario"
            Assert-InstallTest ($state.ShortcutTarget -eq $target) "Shortcut target mismatch in $scenario"
        }
        if ($scenario -eq 'already-current') {
            Assert-InstallTest ([IO.File]::GetLastWriteTimeUtc($target) -eq $oldStamp) 'Up-to-date binary was rewritten'
            Assert-InstallTest ($state.Stopped.Count -eq 0) 'Up-to-date running instance was stopped'
        }
        if ($scenario -eq 'update') {
            Assert-InstallTest ($state.Stopped.Count -eq 1) 'Managed instance was not closed for update'
        }
        if ($scenario -in @('launch', 'bootstrap')) {
            Assert-InstallTest ($state.Started.Count -eq 1 -and $state.Started[0] -eq $target) 'Updated executable was not launched'
        } else {
            Assert-InstallTest ($state.Started.Count -eq 0) "NoStart or failed install launched a process in $scenario"
        }
        if ($scenario -eq 'what-if') { Assert-InstallTest ($state.Downloads -eq 0) 'WhatIf downloaded assets' }
        Assert-InstallTest (@(Get-ChildItem -LiteralPath $installDir -Directory -Filter '.burnbar-update-*').Count -eq 0) "Staging files leaked in $scenario"
        Write-Host "PASS: $scenario"
    }
} finally {
    $env:APPDATA = $previousAppData
    $env:LOCALAPPDATA = $previousLocalAppData
    Remove-Variable -Name BurnBarInstallerTestState -Scope Global -ErrorAction SilentlyContinue
    $resolvedRoot = [IO.Path]::GetFullPath($testRoot)
    if (-not $resolvedRoot.StartsWith($buildRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase) -or
        (Split-Path $resolvedRoot -Leaf) -notlike 'installer-tests-*') {
        throw 'Refusing to clean up a test directory outside .build.'
    }
    Remove-Item -LiteralPath $resolvedRoot -Recurse -Force
}
Write-Host 'All installer checks passed.'
