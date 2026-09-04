<#
.SYNOPSIS
    Captures a screenshot of a running window for the documentation site.
.PARAMETER ProcessName
    Process whose main window should be captured.
.PARAMETER Out
    Destination PNG path.
#>
param(
    [Parameter(Mandatory = $true)][string]$ProcessName,
    [Parameter(Mandatory = $true)][string]$Out,
    [int]$WaitSeconds = 3
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing

Add-Type @"
using System;
using System.Runtime.InteropServices;
public class WinCapture {
    // Without this the capture host is DPI-virtualised: window coordinates come
    // back in scaled units and the screenshot is a blurry upscale.
    [DllImport("user32.dll")] public static extern bool SetProcessDPIAware();
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT lpRect);
    // PW_RENDERFULLCONTENT (2) makes a composited window redraw itself into the
    // supplied DC, so the capture cannot pick up whatever else is on screen.
    [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr hWnd, IntPtr hdc, uint flags);
    [DllImport("dwmapi.dll")] public static extern int DwmGetWindowAttribute(IntPtr hWnd, int attr, out RECT value, int size);
    [StructLayout(LayoutKind.Sequential)]
    public struct RECT { public int Left, Top, Right, Bottom; }
}
"@

[void][WinCapture]::SetProcessDPIAware()

Start-Sleep -Seconds $WaitSeconds

$proc = Get-Process -Name $ProcessName -ErrorAction SilentlyContinue |
        Where-Object { $_.MainWindowHandle -ne 0 } | Select-Object -First 1
if (-not $proc) { throw "No window found for process '$ProcessName'" }

$handle = $proc.MainWindowHandle
[void][WinCapture]::ShowWindow($handle, 9)   # SW_RESTORE
[void][WinCapture]::SetForegroundWindow($handle)
Start-Sleep -Milliseconds 900

# The extended frame bounds exclude the invisible resize border, so the capture
# is tight to what the user actually sees.
$rect = New-Object WinCapture+RECT
$size = [System.Runtime.InteropServices.Marshal]::SizeOf($rect)
if ([WinCapture]::DwmGetWindowAttribute($handle, 9, [ref]$rect, $size) -ne 0) {
    [void][WinCapture]::GetWindowRect($handle, [ref]$rect)
}

$width = $rect.Right - $rect.Left
$height = $rect.Bottom - $rect.Top
if ($width -le 0 -or $height -le 0) { throw "Window has no usable size" }

$bmp = New-Object System.Drawing.Bitmap $width, $height
$g = [System.Drawing.Graphics]::FromImage($bmp)
$hdc = $g.GetHdc()
$printed = [WinCapture]::PrintWindow($handle, $hdc, 2)
$g.ReleaseHdc($hdc)
if (-not $printed) {
    # Very old compositors ignore PrintWindow; fall back to reading the screen,
    # which needs the window genuinely in front.
    $g.CopyFromScreen($rect.Left, $rect.Top, 0, 0, $bmp.Size)
}
$g.Dispose()

# The reported window bounds include the invisible resize frame, which comes
# out as a pure black margin. Trim it so the image is tight to the app.
function Get-ContentBounds($bitmap) {
    $left = 0; $top = 0
    $right = $bitmap.Width - 1; $bottom = $bitmap.Height - 1

    $isBlankColumn = {
        param($x)
        for ($y = 0; $y -lt $bitmap.Height; $y += 4) {
            $p = $bitmap.GetPixel($x, $y)
            if ($p.R -ne 0 -or $p.G -ne 0 -or $p.B -ne 0) { return $false }
        }
        return $true
    }
    $isBlankRow = {
        param($y)
        for ($x = 0; $x -lt $bitmap.Width; $x += 4) {
            $p = $bitmap.GetPixel($x, $y)
            if ($p.R -ne 0 -or $p.G -ne 0 -or $p.B -ne 0) { return $false }
        }
        return $true
    }

    while ($left -lt $right -and (& $isBlankColumn $left)) { $left++ }
    while ($right -gt $left -and (& $isBlankColumn $right)) { $right-- }
    while ($top -lt $bottom -and (& $isBlankRow $top)) { $top++ }
    while ($bottom -gt $top -and (& $isBlankRow $bottom)) { $bottom-- }

    return New-Object System.Drawing.Rectangle $left, $top, ($right - $left + 1), ($bottom - $top + 1)
}

$crop = Get-ContentBounds $bmp
$final = $bmp
if ($crop.Width -gt 100 -and $crop.Height -gt 100 -and
    ($crop.Width -ne $bmp.Width -or $crop.Height -ne $bmp.Height)) {
    $final = $bmp.Clone($crop, $bmp.PixelFormat)
}

$dir = Split-Path -Parent $Out
if ($dir -and -not (Test-Path $dir)) { $null = New-Item -ItemType Directory -Path $dir -Force }
$final.Save($Out, [System.Drawing.Imaging.ImageFormat]::Png)

Write-Output "Captured $($final.Width)x$($final.Height) to $Out"
if ($final -ne $bmp) { $final.Dispose() }
$bmp.Dispose()
