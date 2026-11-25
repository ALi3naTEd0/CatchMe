# Builds CatchMe for Windows x64 with the download engine inside, and writes
# to dist\:
#   CatchMe-<version>-windows-x64-setup.exe   installer (no admin rights needed)
#   CatchMe-<version>-windows-x64.zip         portable
$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..\..')

$version = $env:VERSION
if (-not $version) {
  $version = ((Select-String -Path pubspec.yaml -Pattern '^version:').Line -replace 'version:\s*', '' -replace '\+.*', '')
}
Write-Host "== CatchMe $version for Windows"

flutter build windows --release --dart-define=CATCHME_VERSION=$version
if ($LASTEXITCODE -ne 0) { throw 'flutter build failed' }
$bundle = 'build\windows\x64\runner\Release'

# -H=windowsgui: no console window flashes when the app starts the engine.
Push-Location catchmed
$env:CGO_ENABLED = '0'
go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=$version" -o "..\$bundle\catchmed.exe" .\cmd\catchmed
if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
Pop-Location

New-Item -ItemType Directory -Force dist | Out-Null
Compress-Archive -Path "$bundle\*" -DestinationPath "dist\CatchMe-$version-windows-x64.zip" -Force

$iscc = (Get-Command iscc -ErrorAction SilentlyContinue)?.Source
if (-not $iscc) { $iscc = "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe" }
if (-not (Test-Path $iscc)) { throw 'Inno Setup not found (choco install innosetup)' }
& $iscc /Qp "/DAppVersion=$version" "/DSourceDir=$((Resolve-Path $bundle).Path)" "/O$((Resolve-Path dist).Path)" packaging\windows\catchme.iss
if ($LASTEXITCODE -ne 0) { throw 'installer build failed' }

Get-ChildItem dist
