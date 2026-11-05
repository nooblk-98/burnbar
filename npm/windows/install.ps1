#requires -Version 5.1
<#
.SYNOPSIS
Installs or updates a stable or development BurnBar release for the current user.
.NOTES
Internal npx helper. Run `npx --yes burnbar@development install` instead.
#>
[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Programs\BurnBar'),
    [switch]$NoStart,
    [ValidateSet('stable', 'development')]
    [string]$Channel = 'stable'
)

$ErrorActionPreference = 'Stop'
if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
    throw 'BurnBar requires Windows.'
}
if (-not [Environment]::Is64BitOperatingSystem) {
    throw 'The BurnBar release requires 64-bit Windows.'
}

$installPath = [IO.Path]::GetFullPath([Environment]::ExpandEnvironmentVariables($InstallDir))
$targetExe = Join-Path $installPath 'burnbar.exe'
$headers = @{ 'User-Agent' = 'BurnBar-Installer'; Accept = 'application/vnd.github+json' }
$previousTls = [Net.ServicePointManager]::SecurityProtocol
$stagePath = $null
$preserveBackup = $false
$stoppedPaths = @()
$installComplete = $false

function Get-BurnBarProcesses {
    param([string]$Target, [bool]$TargetOnly)
    $session = [Diagnostics.Process]::GetCurrentProcess().SessionId
    foreach ($candidate in @(Get-Process -Name 'burnbar*' -ErrorAction SilentlyContinue)) {
        try {
            if ($candidate.SessionId -ne $session -or -not $candidate.Path) { continue }
            $candidatePath = [IO.Path]::GetFullPath($candidate.Path)
            if ($candidatePath -eq $Target) {
                $candidate
            } elseif (-not $TargetOnly) {
                $info = [Diagnostics.FileVersionInfo]::GetVersionInfo($candidatePath)
                if ($info.ProductName -eq 'BurnBar' -and $info.OriginalFilename -eq 'burnbar.exe') {
                    $candidate
                }
            }
        } catch {
            # Never stop an inaccessible or unverified process based only on its name.
            continue
        }
    }
}

try {
    [Net.ServicePointManager]::SecurityProtocol = $previousTls -bor [Net.SecurityProtocolType]::Tls12
    Write-Host "Checking the latest BurnBar $Channel release..."
    if ($Channel -eq 'stable') {
        $release = Invoke-RestMethod -Uri 'https://api.github.com/repos/nooblk-98/burnbar/releases/latest' -Headers $headers -TimeoutSec 30
        if ($release.draft -or $release.prerelease -or -not $release.tag_name) {
            throw 'GitHub did not return a published stable release.'
        }
    } else {
        # Each development build has immutable assets. Never update two assets on
        # a rolling release, where concurrent clients could fetch a mixed pair.
        $release = $null
        for ($page = 1; $page -le 20; $page++) {
            $releases = @((Invoke-RestMethod -Uri "https://api.github.com/repos/nooblk-98/burnbar/releases?per_page=100&page=$page" -Headers $headers -TimeoutSec 30))
            $release = $releases | Where-Object { -not $_.draft -and $_.prerelease -and $_.tag_name -match '^dev-[0-9]+-[0-9]+$' } | Select-Object -First 1
            if ($release -or $releases.Count -lt 100) { break }
        }
        if (-not $release) { throw 'No published development build was found. Try the stable channel or wait for the development workflow.' }
        Write-Warning 'Installing an unreleased development build. Switch to --channel stable to return to a stable release.'
    }
    $exeAssets = @($release.assets | Where-Object { $_.name -ceq 'burnbar.exe' })
    $hashAssets = @($release.assets | Where-Object { $_.name -ceq 'burnbar.exe.sha256' })
    if ($exeAssets.Count -ne 1 -or $hashAssets.Count -ne 1) {
        throw "Release $($release.tag_name) must contain burnbar.exe and burnbar.exe.sha256."
    }
    foreach ($asset in @($exeAssets[0], $hashAssets[0])) {
        $assetUri = [Uri]$asset.browser_download_url
        $expectedAssetPath = '/nooblk-98/burnbar/releases/download/' + [Uri]::EscapeDataString($release.tag_name) + '/' + $asset.name
        if ($assetUri.Scheme -ne 'https' -or $assetUri.Host -ne 'github.com' -or
            $assetUri.AbsolutePath -cne $expectedAssetPath -or $assetUri.Query -or $assetUri.Fragment) {
            throw 'The release contains an unexpected download URL.'
        }
    }
    if (-not $PSCmdlet.ShouldProcess($targetExe, "Install or update BurnBar $($release.tag_name)")) {
        return
    }

    [IO.Directory]::CreateDirectory($installPath) | Out-Null
    $stagePath = Join-Path $installPath ('.burnbar-update-' + [Guid]::NewGuid().ToString('N'))
    [IO.Directory]::CreateDirectory($stagePath) | Out-Null
    $downloadExe = Join-Path $stagePath 'burnbar.exe'
    $downloadHash = Join-Path $stagePath 'burnbar.exe.sha256'
    $backupExe = Join-Path $stagePath 'burnbar.previous.exe'
    Write-Host "Downloading BurnBar $($release.tag_name)..."
    Invoke-WebRequest -UseBasicParsing -Uri $hashAssets[0].browser_download_url -Headers $headers -OutFile $downloadHash -TimeoutSec 60 | Out-Null
    Invoke-WebRequest -UseBasicParsing -Uri $exeAssets[0].browser_download_url -Headers $headers -OutFile $downloadExe -TimeoutSec 180 | Out-Null
    $hashText = (Get-Content -LiteralPath $downloadHash -Raw).Trim()
    if ($hashText -notmatch '\A([0-9a-fA-F]{64})[ \t]+\*?burnbar\.exe\z') {
        throw 'The release checksum file has an unexpected format.'
    }
    $expectedHash = $Matches[1].ToLowerInvariant()
    $actualHash = (Get-FileHash -LiteralPath $downloadExe -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualHash -ne $expectedHash) {
        throw 'SHA-256 verification failed. The existing installation was not changed.'
    }
    $digest = $exeAssets[0].digest
    if ($digest -and $digest -ne ('sha256:' + $actualHash)) {
        throw 'The download does not match the release asset digest. Installation was stopped.'
    }

    $sameFile = (Test-Path -LiteralPath $targetExe -PathType Leaf) -and
        ((Get-FileHash -LiteralPath $targetExe -Algorithm SHA256).Hash.ToLowerInvariant() -eq $actualHash)
    $targetRunning = $false
    foreach ($running in @(Get-BurnBarProcesses -Target $targetExe -TargetOnly $NoStart.IsPresent)) {
        if ($running.HasExited) { continue }
        if ($sameFile -and $running.Path -eq $targetExe) {
            $targetRunning = $true
            continue
        }
        $runningPath = $running.Path
        Write-Host 'Closing the running BurnBar instance...'
        $closed = $running.CloseMainWindow() -and $running.WaitForExit(3000)
        if (-not $closed) {
            Stop-Process -Id $running.Id -Force -ErrorAction Stop
            if (-not $running.WaitForExit(5000)) { throw 'BurnBar did not exit; the update was stopped.' }
        }
        $stoppedPaths += $runningPath
    }
    if (-not $sameFile) {
        if (Test-Path -LiteralPath $targetExe -PathType Leaf) {
            [IO.File]::Replace($downloadExe, $targetExe, $backupExe)
        } else {
            [IO.File]::Move($downloadExe, $targetExe)
        }
    }
    $installComplete = $true

    try {
        $programs = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs'
        [IO.Directory]::CreateDirectory($programs) | Out-Null
        $shell = New-Object -ComObject WScript.Shell
        $shortcut = $shell.CreateShortcut((Join-Path $programs 'BurnBar.lnk'))
        $shortcut.TargetPath = $targetExe
        $shortcut.WorkingDirectory = $installPath
        $shortcut.IconLocation = "$targetExe,0"
        $shortcut.Description = 'Claude and Codex usage monitor for the Windows notification area'
        $shortcut.Save()
    } catch {
        Write-Warning "BurnBar is installed, but its Start menu shortcut could not be created: $($_.Exception.Message)"
    }
    if (-not $NoStart -and -not $targetRunning) {
        try {
            $started = Start-Process -FilePath $targetExe -WorkingDirectory $installPath -WindowStyle Hidden -PassThru
            Start-Sleep -Milliseconds 1000
            if ($started.HasExited) {
                Write-Warning 'BurnBar exited after launch. Another copy may be running; close it and launch the installed executable.'
            }
        } catch {
            Write-Warning "BurnBar is installed, but Windows could not launch it: $($_.Exception.Message)"
            Write-Warning 'See the README for unsigned-app and Smart App Control guidance.'
        }
    }
    if ($sameFile) {
        Write-Host "BurnBar $($release.tag_name) is already up to date."
    } else {
        Write-Host "Installed BurnBar $($release.tag_name)."
    }
    Write-Host "Location: $targetExe"
} catch {
    $installError = $_
    if ($stagePath -and (Test-Path -LiteralPath (Join-Path $stagePath 'burnbar.previous.exe'))) {
        try {
            [IO.File]::Copy((Join-Path $stagePath 'burnbar.previous.exe'), $targetExe, $true)
        } catch {
            $preserveBackup = $true
            Write-Warning "The previous executable is preserved at $stagePath\burnbar.previous.exe."
        }
    }
    if (-not $NoStart -and -not $installComplete) {
        foreach ($oldPath in @($stoppedPaths | Select-Object -Unique)) {
            try { Start-Process -FilePath $oldPath -WindowStyle Hidden | Out-Null } catch {
                Write-Warning "Could not restart the previous BurnBar instance at $oldPath."
            }
        }
    }
    throw $installError
} finally {
    [Net.ServicePointManager]::SecurityProtocol = $previousTls
    if ($stagePath) {
        # Remove only our explicitly named files, then the empty staging directory.
        # No recursive deletion or paths outside the unique staging directory.
        foreach ($name in @('burnbar.exe', 'burnbar.exe.sha256')) {
            Remove-Item -LiteralPath (Join-Path $stagePath $name) -Force -ErrorAction SilentlyContinue
        }
        if (-not $preserveBackup) {
            Remove-Item -LiteralPath (Join-Path $stagePath 'burnbar.previous.exe') -Force -ErrorAction SilentlyContinue
            try { [IO.Directory]::Delete($stagePath) } catch { }
        }
    }
}
