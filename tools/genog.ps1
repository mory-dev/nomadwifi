<#
.SYNOPSIS
    Generates the 1200x630 Open Graph card for the site.
.DESCRIPTION
    Kept as a script rather than a committed-once image so the card follows the
    generated icon and the shipped version string instead of drifting from them.
#>
param(
    [string]$Out = "$PSScriptRoot\..\site\assets\og.png",
    [string]$Version = '1.2.0'
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.Windows.Forms

$W = 1200; $H = 630
$bmp = New-Object System.Drawing.Bitmap $W, $H
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
$g.TextRenderingHint = [System.Drawing.Text.TextRenderingHint]::ClearTypeGridFit

$bg = [System.Drawing.ColorTranslator]::FromHtml('#0B0F14')
$g.Clear($bg)

# A soft azure wash behind the mark, so the card is not a flat rectangle.
$glow = New-Object System.Drawing.Drawing2D.GraphicsPath
$glow.AddEllipse(-160, 180, 900, 900)
$wash = New-Object System.Drawing.Drawing2D.PathGradientBrush $glow
$wash.CenterColor = [System.Drawing.Color]::FromArgb(46, 24, 156, 240)
$wash.SurroundColors = @([System.Drawing.Color]::FromArgb(0, 24, 156, 240))
$g.FillPath($wash, $glow)

$icon = [System.Drawing.Image]::FromFile((Resolve-Path "$PSScriptRoot\..\site\assets\logo-512.png"))
$g.DrawImage($icon, 84, 96, 132, 132)
$icon.Dispose()

$white = New-Object System.Drawing.SolidBrush ([System.Drawing.ColorTranslator]::FromHtml('#E6EDF3'))
$brand = New-Object System.Drawing.SolidBrush ([System.Drawing.ColorTranslator]::FromHtml('#189CF0'))
$muted = New-Object System.Drawing.SolidBrush ([System.Drawing.ColorTranslator]::FromHtml('#9AA7B8'))

$wordmark = New-Object System.Drawing.Font 'Segoe UI', 40, ([System.Drawing.FontStyle]::Bold)
$g.DrawString('Nomad', $wordmark, $white, 236, 128)
$nw = $g.MeasureString('Nomad', $wordmark).Width
$g.DrawString('WiFi', $wordmark, $brand, (236 + $nw - 26), 128)

$head = New-Object System.Drawing.Font 'Segoe UI', 52, ([System.Drawing.FontStyle]::Bold)
$g.DrawString("Stop fighting", $head, $white, 78, 290)
$g.DrawString("hotel Wi-Fi.", $head, $brand, 78, 368)

$sub = New-Object System.Drawing.Font 'Segoe UI', 24, ([System.Drawing.FontStyle]::Regular)
$g.DrawString("Wi-Fi roaming, 5 GHz optimizer and VPN choreography for Windows", $sub, $muted, 82, 470)

$foot = New-Object System.Drawing.Font 'Segoe UI', 20, ([System.Drawing.FontStyle]::Regular)
# Built from a code point: PowerShell 5.1 reads .ps1 files as ANSI, so a
# literal middle dot in the source arrives as mojibake.
$dot = [char]0x00B7
$g.DrawString("nomadwifi.mory.dev  $dot  v$Version  $dot  free and open source", $foot, $muted, 82, 530)

$g.Dispose()
$bmp.Save($Out, [System.Drawing.Imaging.ImageFormat]::Png)
Write-Output "wrote $Out ($($bmp.Width)x$($bmp.Height))"
$bmp.Dispose()
