# Smoke test of the Windows installer on a clean CI machine:
#   1. the installer runs silently, without admin rights
#   2. the installed app finds its engine and connects (report file)
#   3. a screenshot is saved for people who cannot run Windows builds
#   4. closing the app also stops the engine
#   5. the uninstaller removes it
# Usage: packaging\windows\smoke.ps1 dist\CatchMe-x.y.z-windows-x64-setup.exe
param([Parameter(Mandatory)] [string] $Installer, [string] $Out = 'dist')
$ErrorActionPreference = 'Stop'
$Out = (Resolve-Path $Out).Path
$report = Join-Path $Out 'smoke-windows.json'
Remove-Item $report -ErrorAction SilentlyContinue

Start-Process $Installer -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/CURRENTUSER', '/NORESTART' -Wait
$dir = Join-Path $env:LOCALAPPDATA 'Programs\CatchMe'
if (-not (Test-Path "$dir\catchme.exe")) { throw "FAIL: not installed in $dir" }
if (-not (Test-Path "$dir\catchmed.exe")) { throw 'FAIL: the engine was not installed' }

$env:CATCHME_SMOKE_FILE = $report
$app = Start-Process "$dir\catchme.exe" -PassThru
for ($i = 0; $i -lt 60 -and -not (Test-Path $report); $i++) { Start-Sleep 1 }
if (-not (Test-Path $report)) { throw 'FAIL: the app did not connect to its engine within 60 s' }
Start-Sleep 1
$r = Get-Content $report -Raw
Write-Host $r
if (($r | ConvertFrom-Json).connected -ne $true) { throw 'FAIL: not connected' }
if (-not (Get-Process catchmed -ErrorAction SilentlyContinue)) { throw 'FAIL: engine process not running' }

Start-Sleep 3 # let the window paint
Add-Type -AssemblyName System.Windows.Forms, System.Drawing
$b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
$bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size)
$bmp.Save((Join-Path $Out 'screenshot-windows.png'), [System.Drawing.Imaging.ImageFormat]::Png)

Stop-Process -Id $app.Id
for ($i = 0; $i -lt 15 -and (Get-Process catchmed -ErrorAction SilentlyContinue); $i++) { Start-Sleep 1 }
if (Get-Process catchmed -ErrorAction SilentlyContinue) { throw 'FAIL: the engine kept running after the app closed' }

Start-Process "$dir\unins000.exe" -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART' -Wait
Start-Sleep 2
if (Test-Path "$dir\catchme.exe") { throw 'FAIL: uninstall left the app behind' }
Write-Host 'OK: Windows installer installs, starts, connects, cleans up and uninstalls'
