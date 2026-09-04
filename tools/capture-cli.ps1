<#
.SYNOPSIS
    Renders a NomadWiFi command's real terminal output to a PNG.
.DESCRIPTION
    Runs the shipped CLI, captures its actual stdout with the ANSI colour
    sequences intact, and paints it into a terminal-styled image. Rendering
    beats screenshotting a live window: it needs no foreground focus, produces
    a deterministic crop at a fixed size, and cannot disturb the terminal the
    developer is sitting in.
.PARAMETER Command
    Arguments to pass to nomadwifi.exe, e.g. "scan" or "vpn status".
.PARAMETER Out
    Destination PNG path.
#>
param(
    [Parameter(Mandatory = $true)][string]$Command,
    [Parameter(Mandatory = $true)][string]$Out,
    [string]$Title = 'nomadwifi',
    [int]$MinColumns = 100
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.Windows.Forms

$exe = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\dist\cli\nomadwifi.exe'))
if (-not (Test-Path $exe)) { throw "Build the CLI first: $exe is missing" }

# --- run the real command -------------------------------------------------
$tmp = [System.IO.Path]::GetTempFileName()
$proc = Start-Process -FilePath $exe -ArgumentList $Command -NoNewWindow -Wait -PassThru `
    -RedirectStandardOutput $tmp
$raw = [System.IO.File]::ReadAllText($tmp, [System.Text.Encoding]::UTF8)
Remove-Item $tmp -Force
if ([string]::IsNullOrWhiteSpace($raw)) { throw "'$Command' produced no output (exit $($proc.ExitCode))" }

# --- terminal palette -----------------------------------------------------
$bg      = [System.Drawing.ColorTranslator]::FromHtml('#0B0F14')
$chrome  = [System.Drawing.ColorTranslator]::FromHtml('#151B23')
$border  = [System.Drawing.ColorTranslator]::FromHtml('#232C38')
$palette = @{
    0  = '#C9D1D9'; 31 = '#F87171'; 32 = '#34D399'; 33 = '#FBBF24'
    34 = '#60A5FA'; 35 = '#A78BFA'; 36 = '#22D3EE'; 37 = '#E6EDF3'; 90 = '#7A8798'
}
$ink = @{}
foreach ($k in $palette.Keys) { $ink[$k] = [System.Drawing.ColorTranslator]::FromHtml($palette[$k]) }

# --- parse SGR runs -------------------------------------------------------
$lines = @()
$fg = 0; $bold = $false
foreach ($line in ($raw -replace "`r", '') -split "`n") {
    $runs = @()
    $pos = 0
    foreach ($m in [regex]::Matches($line, "\x1b\[([0-9;]*)m")) {
        if ($m.Index -gt $pos) {
            $runs += [pscustomobject]@{ Text = $line.Substring($pos, $m.Index - $pos); Fg = $fg; Bold = $bold }
        }
        foreach ($code in ($m.Groups[1].Value -split ';')) {
            switch ($code) {
                ''  { $fg = 0; $bold = $false }
                '0' { $fg = 0; $bold = $false }
                '1' { $bold = $true }
                default { if ($palette.ContainsKey([int]$code)) { $fg = [int]$code } }
            }
        }
        $pos = $m.Index + $m.Length
    }
    if ($pos -lt $line.Length) {
        $runs += [pscustomobject]@{ Text = $line.Substring($pos); Fg = $fg; Bold = $bold }
    }
    $lines += , $runs
}

# Trim leading and trailing blank lines so the frame hugs the content.
$isBlank = { param($r) -not ($r | Where-Object { $_.Text.Trim() -ne '' }) }
while ($lines.Count -and (& $isBlank $lines[0]))                  { $lines = $lines[1..($lines.Count - 1)] }
while ($lines.Count -and (& $isBlank $lines[$lines.Count - 1]))   { $lines = $lines[0..($lines.Count - 2)] }
if (-not $lines.Count) { throw "'$Command' produced only blank lines" }

$columns = $MinColumns
foreach ($runs in $lines) {
    $w = 0
    foreach ($r in $runs) { $w += $r.Text.Length }
    if ($w -gt $columns) { $columns = $w }
}

# --- metrics --------------------------------------------------------------
$fontName = 'Cascadia Mono'
try { $probe = New-Object System.Drawing.Font $fontName, 15.0 } catch { $fontName = 'Consolas' }
if ($probe) { $probe.Dispose() }

$regular = New-Object System.Drawing.Font $fontName, 15.0, ([System.Drawing.FontStyle]::Regular)
$boldF   = New-Object System.Drawing.Font $fontName, 15.0, ([System.Drawing.FontStyle]::Bold)

# GDI text is measured and drawn on whole pixels. That matters here: the banner
# is built from full-block glyphs, and GDI+'s sub-pixel advances leave hairline
# gaps between them that read as an outline instead of a solid letter.
$flags = [System.Windows.Forms.TextFormatFlags]::NoPadding -bor `
         [System.Windows.Forms.TextFormatFlags]::NoPrefix -bor `
         [System.Windows.Forms.TextFormatFlags]::SingleLine
$probeSize = [System.Windows.Forms.TextRenderer]::MeasureText(('M' * 100), $regular,
    (New-Object System.Drawing.Size 10000, 100), $flags)
$cellW = [int][math]::Round($probeSize.Width / 100.0)
# Exactly the font's line box, as a terminal does. Any extra leading opens
# horizontal seams across the block-drawing banner.
$cellH = $probeSize.Height

$padX = 26; $padY = 20; $barH = 40
$width  = ($columns * $cellW) + ($padX * 2)
$height = ($lines.Count * $cellH) + ($padY * 2) + $barH

# --- paint ----------------------------------------------------------------
$bmp = New-Object System.Drawing.Bitmap $width, $height
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
$g.TextRenderingHint = [System.Drawing.Text.TextRenderingHint]::ClearTypeGridFit
$g.Clear($bg)

# window chrome: title bar, traffic lights, centred title
$g.FillRectangle((New-Object System.Drawing.SolidBrush $chrome), 0, 0, $width, $barH)
$g.DrawLine((New-Object System.Drawing.Pen $border), 0, $barH, $width, $barH)
$dots = @('#FF5F57', '#FEBC2E', '#28C840')
for ($i = 0; $i -lt 3; $i++) {
    $c = [System.Drawing.ColorTranslator]::FromHtml($dots[$i])
    $g.FillEllipse((New-Object System.Drawing.SolidBrush $c), (18 + ($i * 20)), 15, 11, 11)
}
$titleFont = New-Object System.Drawing.Font 'Segoe UI', 10.0, ([System.Drawing.FontStyle]::Regular)
$titleText = "$Title $Command"
$tw = $g.MeasureString($titleText, $titleFont).Width
$g.DrawString($titleText, $titleFont, (New-Object System.Drawing.SolidBrush ([System.Drawing.ColorTranslator]::FromHtml('#8B98A9'))),
    (($width - $tw) / 2), 11)

# U+2588 FULL BLOCK is what the banner is built from, and its glyph stops a few
# pixels short of the top of the line box -- stacked rows then show a seam the
# user never sees in a real terminal, which draws block elements procedurally.
# Painting those cells as rectangles reproduces the terminal's own behaviour.
$FULL_BLOCK = [char]0x2588

$y = $barH + $padY
foreach ($runs in $lines) {
    $col = 0
    foreach ($r in $runs) {
        $f = if ($r.Bold) { $boldF } else { $regular }
        $pen = New-Object System.Drawing.SolidBrush $ink[$r.Fg]

        # Walk the run, emitting block spans as rectangles and the rest as text.
        $i = 0
        while ($i -lt $r.Text.Length) {
            $isBlock = $r.Text[$i] -eq $FULL_BLOCK
            $j = $i
            while ($j -lt $r.Text.Length -and (($r.Text[$j] -eq $FULL_BLOCK) -eq $isBlock)) { $j++ }
            $span = $r.Text.Substring($i, $j - $i)
            $x = $padX + ($col * $cellW)

            if ($isBlock) {
                $g.FillRectangle($pen, $x, $y, ($span.Length * $cellW), $cellH)
            } else {
                [System.Windows.Forms.TextRenderer]::DrawText($g, $span, $f,
                    (New-Object System.Drawing.Point $x, $y), $ink[$r.Fg], $flags)
            }
            $col += $span.Length
            $i = $j
        }
        $pen.Dispose()
    }
    $y += $cellH
}

$g.Dispose()
$dir = Split-Path -Parent $Out
if ($dir -and -not (Test-Path $dir)) { $null = New-Item -ItemType Directory -Path $dir -Force }
$bmp.Save($Out, [System.Drawing.Imaging.ImageFormat]::Png)
Write-Output "Rendered $($bmp.Width)x$($bmp.Height) to $Out"
$bmp.Dispose(); $regular.Dispose(); $boldF.Dispose(); $titleFont.Dispose()
