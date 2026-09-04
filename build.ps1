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

.PARAMETER Installer
    Also build dist/NomadWiFi-Setup-<version>.exe. Requires Inno Setup 6.

.PARAMETER InstallerOnly
    Build only the installer, reusing the existing dist tree. Used after the
    binaries have been signed, so signatures are not thrown away.

.PARAMETER Version
    Version to stamp into the binary and the archive names, with or without a
    leading "v". Defaults to the version in this script; release builds pass
    the pushed tag.
#>
[CmdletBinding()]
param(
    [switch]$SkipGui,
    [switch]$Zip,
    # Build the Inno Setup installer. Kept opt-in so an ordinary dev build does
    # not need Inno installed.
    [switch]$Installer,
    # Compile the installer from whatever is already in dist, skipping the
    # clean and rebuild. The release pipeline needs this because it signs the
    # binaries between building them and wrapping them in the installer -- a
    # full rebuild here would discard those signatures.
    [switch]$InstallerOnly,
    # Release builds pass the pushed tag so the archives and the binary carry
    # the version that was actually shipped.
    [string]$Version
)

$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$dist = Join-Path $root 'dist'

# VERSION is the single source of truth. Everything Windows shows the user --
# versioninfo.json, AssemblyInfo.cs and both side-by-side manifests -- is
# generated from it below, because keeping four hand-edited copies in step is
# exactly the kind of thing that silently drifts.
$versionFile = Join-Path $PSScriptRoot 'VERSION'
$version = if ($Version) { $Version.TrimStart('v') } else { (Get-Content $versionFile -Raw).Trim() }

# Set-Content -Encoding utf8 emits a BOM on PowerShell 5.1, and goversioninfo
# refuses to parse a JSON file that starts with one.
function Write-Utf8NoBom($path, $text) {
    [System.IO.File]::WriteAllText($path, $text, (New-Object System.Text.UTF8Encoding $false))
}

# Windows reads file properties out of resources compiled into each binary, and
# those are declared in two files that would otherwise keep their own hardcoded
# version. Stamping both from one place is what stops a release shipping
# binaries whose properties disagree with the tag they were cut from.
function Set-StampedVersion($ver) {
    $four = $ver
    while (($four -split '\.').Count -lt 4) { $four = "$four.0" }

    $viPath = Join-Path $PSScriptRoot 'cmd\nomadwifi\versioninfo.json'
    $vi = Get-Content $viPath -Raw
    $parts = $ver -split '\.'
    $vi = [regex]::Replace($vi, '"Major":\s*\d+', '"Major": ' + $parts[0])
    $vi = [regex]::Replace($vi, '"Minor":\s*\d+', '"Minor": ' + $parts[1])
    $vi = [regex]::Replace($vi, '"Patch":\s*\d+', '"Patch": ' + $parts[2])
    $vi = [regex]::Replace($vi, '"FileVersion":\s*"[\d.]+"', '"FileVersion": "' + $four + '"')
    $vi = [regex]::Replace($vi, '"ProductVersion":\s*"[\d.]+"', '"ProductVersion": "' + $ver + '"')
    Write-Utf8NoBom $viPath $vi

    $aiPath = Join-Path $PSScriptRoot 'tools\NomadWiFi.UI\Properties\AssemblyInfo.cs'
    $ai = Get-Content $aiPath -Raw
    $ai = [regex]::Replace($ai, 'AssemblyVersion\("[\d.]+"\)', 'AssemblyVersion("' + $four + '")')
    $ai = [regex]::Replace($ai, 'AssemblyFileVersion\("[\d.]+"\)', 'AssemblyFileVersion("' + $four + '")')
    $ai = [regex]::Replace($ai, 'AssemblyInformationalVersion\("[^"]+"\)', 'AssemblyInformationalVersion("' + $ver + '")')
    Write-Utf8NoBom $aiPath $ai

    # Both side-by-side manifests carry their own assemblyIdentity version, and
    # nothing else updates them, so they drift away from the shipped version.
    foreach ($manifest in @('tools\NomadWiFi.UI\app.manifest', 'cmd\nomadwifi\nomadwifi.manifest')) {
        $mPath = Join-Path $PSScriptRoot $manifest
        $m = Get-Content $mPath -Raw
        $m = [regex]::Replace($m, '(<assemblyIdentity[^>]*?version=")[\d.]+(")', ('${1}' + $four + '${2}'))
        Write-Utf8NoBom $mPath $m
    }
}

# Always stamp, not just for releases. When the files already agree this
# rewrites identical bytes and leaves the tree clean, so the guarantee is that
# a committed version can never disagree with VERSION.
if ($Version) { Write-Utf8NoBom $versionFile ($version + "`n") }
Set-StampedVersion $version

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

if ($InstallerOnly) {
    # Reuse the signed tree exactly as it is; the block below would clean it.
    $Installer = $true
} else {

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

}

if ($Zip) {
    Write-Step 'Creating archives'
    Compress-Archive -Path (Join-Path $dist 'NomadWiFi\*') `
        -DestinationPath (Join-Path $dist "NomadWiFi-$version-windows.zip") -Force
    Compress-Archive -Path (Join-Path $dist 'cli\*') `
        -DestinationPath (Join-Path $dist "nomadwifi-cli-$version-windows.zip") -Force
}

if ($Installer) {
    Write-Step 'Building the installer'
    if ($SkipGui) { throw 'The installer needs the desktop app; drop -SkipGui' }

    # Inno Setup 6 is preinstalled on the GitHub windows runners. Locally it may
    # be a per-user install, which is where winget puts it.
    $iscc = (Get-Command iscc -ErrorAction SilentlyContinue).Source
    if (-not $iscc) {
        foreach ($candidate in @(
            "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
            "$env:ProgramFiles\Inno Setup 6\ISCC.exe",
            "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe")) {
            if (Test-Path $candidate) { $iscc = $candidate; break }
        }
    }
    if (-not $iscc) { throw 'Inno Setup 6 was not found. Install it: winget install JRSoftware.InnoSetup' }

    & $iscc "/DAppVersion=$version" (Join-Path $root 'packaging\nomadwifi.iss') | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Installer build failed' }

    $setup = Join-Path $dist "NomadWiFi-Setup-$version.exe"
    if (-not (Test-Path $setup)) { throw "Expected the installer at $setup" }
    Write-Host ("    {0,-34} {1,8} KB" -f "NomadWiFi-Setup-$version.exe",
        [math]::Round((Get-Item $setup).Length / 1KB, 1))
}

Write-Step "Done. Packages are in $dist"
