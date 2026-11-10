# Makes small JPEG copies (at most 1280 px wide) of the window screenshots and zips them with the desktop
# screenshots and fonts.json, so the whole set fits in a CI log (windows-window.sh prints the zip as base64).
# Usage: small-shots.ps1 <results-dir> <zip-file>
param([string]$Results, [string]$Zip)
Add-Type -AssemblyName System.Drawing
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("small-shots-" + [guid]::NewGuid())
New-Item -ItemType Directory -Force $tmp | Out-Null
$jpeg = [System.Drawing.Imaging.ImageCodecInfo]::GetImageEncoders() | Where-Object { $_.MimeType -eq 'image/jpeg' }
$q = New-Object System.Drawing.Imaging.EncoderParameters 1
$q.Param[0] = New-Object System.Drawing.Imaging.EncoderParameter ([System.Drawing.Imaging.Encoder]::Quality), ([long]85)
$files = @(Get-ChildItem (Join-Path $Results 'shots') -Filter *.png) + @(Get-ChildItem $Results -Filter 'desktop-*.png')
foreach ($f in $files) {
  $img = [System.Drawing.Image]::FromFile($f.FullName)
  $w = [Math]::Min(1280, $img.Width); $h = [int]($img.Height * $w / $img.Width)
  $bmp = New-Object System.Drawing.Bitmap $w, $h
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.InterpolationMode = 'HighQualityBicubic'
  $g.DrawImage($img, 0, 0, $w, $h)
  $bmp.Save((Join-Path $tmp ($f.BaseName + '.jpg')), $jpeg, $q)
  $g.Dispose(); $bmp.Dispose(); $img.Dispose()
}
Copy-Item (Join-Path $Results 'shots\fonts.json') $tmp -ErrorAction SilentlyContinue
Compress-Archive -Force -Path (Join-Path $tmp '*') -DestinationPath $Zip
