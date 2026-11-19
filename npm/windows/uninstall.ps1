#requires -Version 5.1
<#
.SYNOPSIS
Removes BurnBar for the current user.
.NOTES
Internal npx helper. Run `npx --yes burnbar@latest uninstall` instead.
#>
[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Programs\BurnBar'),
    [switch]$Purge
)

$ErrorActionPreference = 'Stop'
if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
    throw 'BurnBar requires Windows.'
}

$installPath = [IO.Path]::GetFullPath([Environment]::ExpandEnvironmentVariables($InstallDir))
$targetExe = Join-Path $installPath 'burnbar.exe'
$shortcut = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\BurnBar.lnk'
$settingsDir = Join-Path $env:APPDATA 'BurnBar'

if (-not $PSCmdlet.ShouldProcess($installPath, 'Uninstall BurnBar')) {
    return
}

# Stop only processes whose executable is exactly the installed one.
foreach ($candidate in @(Get-Process -Name 'burnbar*' -ErrorAction SilentlyContinue)) {
    try {
        if (-not $candidate.Path -or [IO.Path]::GetFullPath($candidate.Path) -ne $targetExe) { continue }
        Write-Host 'Closing the running BurnBar instance...'
        if (-not ($candidate.CloseMainWindow() -and $candidate.WaitForExit(3000))) {
            Stop-Process -Id $candidate.Id -Force -ErrorAction Stop
            if (-not $candidate.WaitForExit(5000)) { throw 'BurnBar did not exit; uninstall was stopped.' }
        }
    } catch {
        throw "Could not close BurnBar: $($_.Exception.Message)"
    }
}

$removed = $false
# Remove only the files the installer creates, never the whole directory tree.
foreach ($file in @($targetExe, (Join-Path $installPath 'burnbar.previous.exe'), $shortcut)) {
    if (Test-Path -LiteralPath $file -PathType Leaf) {
        Remove-Item -LiteralPath $file -Force
        Write-Host "Removed $file"
        $removed = $true
    }
}
Get-ChildItem -LiteralPath $installPath -Directory -Filter '.burnbar-update-*' -ErrorAction SilentlyContinue |
    ForEach-Object { Remove-Item -LiteralPath $_.FullName -Recurse -Force -ErrorAction SilentlyContinue }
try { [IO.Directory]::Delete($installPath) } catch { }  # only succeeds when empty

# "Start with Windows" entry, if the user enabled it.
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
foreach ($name in @('BurnBar', 'burnbar')) {
    if (Get-ItemProperty -Path $runKey -Name $name -ErrorAction SilentlyContinue) {
        Remove-ItemProperty -Path $runKey -Name $name -ErrorAction SilentlyContinue
        Write-Host 'Removed the start-with-Windows entry.'
    }
}

if ($Purge) {
    if (Test-Path -LiteralPath $settingsDir -PathType Container) {
        Remove-Item -LiteralPath $settingsDir -Recurse -Force
        Write-Host "Removed settings and history: $settingsDir"
    }
} elseif (Test-Path -LiteralPath $settingsDir) {
    Write-Host "Settings and history were kept in $settingsDir. Use --purge to delete them."
}

if ($removed) { Write-Host 'BurnBar has been uninstalled.' } else { Write-Host 'BurnBar was not found; nothing to uninstall.' }
