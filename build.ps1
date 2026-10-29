# Builds bin\claude-usage.exe: runs vet and tests, then compiles.
# Usage: .\build.ps1 [-Version 0.2.0] [-Resources] [-SkipTests]
#   -Resources  regenerate cmd\claude-usage\rsrc_windows_amd64.syso (icon, manifest, version info);
#               only needed when the icon or version changes - the generated file is committed.
param(
    [string]$Version = "0.2.0",
    [switch]$Resources,
    [switch]$SkipTests
)
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
$work = Join-Path $PSScriptRoot ".build"
New-Item -ItemType Directory -Force $work, (Join-Path $PSScriptRoot "bin") | Out-Null

function Step($name, [scriptblock]$cmd) {
    Write-Host "==> $name" -ForegroundColor Cyan
    & $cmd
    if ($LASTEXITCODE -ne 0) { throw "$name failed (exit $LASTEXITCODE)" }
}

# Helper binaries are built into .build\ and launched from there, because Smart App Control can
# block unsigned executables that `go run` / `go test` start from the temp folder. If it blocks
# one anyway, the step is skipped with a warning (CI runs them normally).
function RunLocal($exe, $argList) {
    try {
        $p = Start-Process -FilePath $exe -ArgumentList $argList -WorkingDirectory $PSScriptRoot -NoNewWindow -Wait -PassThru -ErrorAction Stop
        $global:LASTEXITCODE = $p.ExitCode
    } catch {
        Write-Warning "$(Split-Path $exe -Leaf) was blocked from running; skipping."
        $global:LASTEXITCODE = 0
    }
}

Step "vet" { go vet ./... }
if ($Resources) {
    Step "resources" {
        go build -o "$work\genres.exe" ./tools/genres
        if ($LASTEXITCODE -eq 0) { RunLocal "$work\genres.exe" $Version }
    }
}
if (-not $SkipTests) {
    Step "test" {
        go test -c -o "$work\" ./...
        if ($LASTEXITCODE -eq 0) {
            foreach ($t in Get-ChildItem "$work\*.test.exe") {
                RunLocal $t.FullName "-test.v"
                if ($LASTEXITCODE -ne 0) { break }
            }
        }
    }
}

$running = Get-Process claude-usage -ErrorAction SilentlyContinue
if ($running) { Write-Host "Stopping running meter..."; $running | Stop-Process; Start-Sleep -Milliseconds 500 }
Step "build" { go build -trimpath -ldflags "-H windowsgui -s -w -X main.version=$Version" -o bin\claude-usage.exe ./cmd/claude-usage }
Remove-Item $work -Recurse -Force -ErrorAction SilentlyContinue
Write-Host "Built bin\claude-usage.exe $Version" -ForegroundColor Green
