<#
.SYNOPSIS
    Builds NomadWiFi and lays out the shippable distribution.

.DESCRIPTION
    Produces two packages from one source tree:

      dist/NomadWiFi/nomadwifi.exe       the desktop app
      dist/NomadWiFi/core/nomadwifi.exe  the engine it drives
      dist/cli/nomadwifi.exe             the standalone command line

    Both executables are named nomadwifi.exe on purpose. The app resolves its
    core from core\nomadwifi.exe and refuses any candidate equal to its own
    path, so it can never launch itself.

.PARAMETER SkipGui
    Build only the command line. Useful when MSBuild is unavailable.

.PARAMETER Zip
    Also produce dist/*.zip archives.

.PARAMETER Version
    Version to stamp into the binary and the archive names, with or without a
    leading "v". Defaults to the version in this script; release builds pass
    the pushed tag.
#>
[CmdletBinding()]
param(
    [switch]$SkipGui,
    [switch]$Zip,
    # Release builds pass the pushed tag so the archives and the binary carry
    # the version that was actually shipped.
    [string]$Version
)

$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$dist = Join-Path $root 'dist'

$version = if ($Version) { $Version.TrimStart('v') } else { '1.2.0' }

function Write-Step($message) {
    Write-Host "==> $message" -ForegroundColor Cyan
}

# Locate the Go toolchain, which is not always on PATH.
$go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $go) {
    $candidate = 'C:\Program Files\Go\bin\go.exe'
    if (Test-Path $candidate) { $go = $candidate }
}
if (-not $go) { throw 'Go was not found. Install it from https://go.dev/dl/' }

Write-Step 'Cleaning dist'
if (Test-Path $dist) { Remove-Item $dist -Recurse -Force }
$null = New-Item -ItemType Directory -Path (Join-Path $dist 'NomadWiFi\core') -Force
$null = New-Item -ItemType Directory -Path (Join-Path $dist 'cli') -Force

Write-Step 'Regenerating the application icon'
& $go run ./tools/genicon
if ($LASTEXITCODE -ne 0) { throw 'Icon generation failed' }

# resource_windows.syso carries the icon and version info into the Go binary.
# It is committed so a plain `go build` needs no tooling, which also means it
# goes stale silently whenever the logo changes -- so refresh it when we can.
$goversioninfo = Get-Command goversioninfo -ErrorAction SilentlyContinue
if (-not $goversioninfo) {
    $candidate = Join-Path $env:USERPROFILE 'go\bin\goversioninfo.exe'
    if (Test-Path $candidate) { $goversioninfo = $candidate }
}
if ($goversioninfo) {
    & $goversioninfo -o cmd/nomadwifi/resource_windows.syso cmd/nomadwifi/versioninfo.json
    if ($LASTEXITCODE -ne 0) { throw 'Version resource generation failed' }
} else {
    Write-Host '    goversioninfo not installed; reusing the committed resource_windows.syso' -ForegroundColor DarkYellow
}

Write-Step 'Running tests'
& $go test ./...
if ($LASTEXITCODE -ne 0) { throw 'Tests failed' }

Write-Step 'Building the command line core'
$ldflags = "-s -w -X main.Version=$version"
$cliOut = Join-Path $dist 'cli\nomadwifi.exe'
& $go build -trimpath -ldflags $ldflags -o $cliOut ./cmd/nomadwifi
if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
Copy-Item $cliOut (Join-Path $dist 'NomadWiFi\core\nomadwifi.exe') -Force

if (-not $SkipGui) {
    Write-Step 'Building the desktop app'
    $msbuild = 'C:\Windows\Microsoft.NET\Framework64\v4.0.30319\MSBuild.exe'
    if (-not (Test-Path $msbuild)) { throw "MSBuild was not found at $msbuild" }

    $project = Join-Path $root 'tools\NomadWiFi.UI\NomadWiFi.UI.csproj'
    & $msbuild $project /p:Configuration=Release /t:Rebuild /v:minimal /nologo
    if ($LASTEXITCODE -ne 0) { throw 'GUI build failed' }

    $guiExe = Join-Path $root 'build\gui\nomadwifi.exe'
    if (-not (Test-Path $guiExe)) { throw "Expected the GUI at $guiExe" }
    Copy-Item $guiExe (Join-Path $dist 'NomadWiFi\nomadwifi.exe') -Force
}

Copy-Item (Join-Path $root 'LICENSE') (Join-Path $dist 'NomadWiFi\LICENSE.txt') -Force
Copy-Item (Join-Path $root 'LICENSE') (Join-Path $dist 'cli\LICENSE.txt') -Force

Write-Step 'Verifying the layout'
$expected = @('NomadWiFi\nomadwifi.exe', 'NomadWiFi\core\nomadwifi.exe', 'cli\nomadwifi.exe')
foreach ($relative in $expected) {
    if ($SkipGui -and $relative -eq 'NomadWiFi\nomadwifi.exe') { continue }
    $path = Join-Path $dist $relative
    if (-not (Test-Path $path)) { throw "Missing $relative" }
    $kb = [math]::Round((Get-Item $path).Length / 1KB, 1)
    Write-Host ("    {0,-34} {1,8} KB" -f $relative, $kb)
}

# The app and its core share a name, so confirm they are genuinely different
# builds and not the same file copied into both places.
$app = Join-Path $dist 'NomadWiFi\nomadwifi.exe'
$core = Join-Path $dist 'NomadWiFi\core\nomadwifi.exe'
if ((Test-Path $app) -and (Test-Path $core)) {
    $appHash = (Get-FileHash $app).Hash
    $coreHash = (Get-FileHash $core).Hash
    if ($appHash -eq $coreHash) { throw 'The app and its core are the same binary' }
    Write-Host '    app and core are distinct binaries' -ForegroundColor Green
}

if ($Zip) {
    Write-Step 'Creating archives'
    Compress-Archive -Path (Join-Path $dist 'NomadWiFi\*') `
        -DestinationPath (Join-Path $dist "NomadWiFi-$version-windows.zip") -Force
    Compress-Archive -Path (Join-Path $dist 'cli\*') `
        -DestinationPath (Join-Path $dist "nomadwifi-cli-$version-windows.zip") -Force
}

Write-Step "Done. Packages are in $dist"
