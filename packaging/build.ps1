# Builds cupbox.exe, the Windows installer and the portable zip.
# Needs: Go 1.27+, the FFmpeg DLLs in the repository root (see
# .tools/ffmpeg-min), NSIS at the default path.
$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root

go build -ldflags="-H windowsgui" -o cupbox.exe .

$makensis = "C:\Program Files (x86)\NSIS\makensis.exe"
if (Test-Path $makensis) {
    & $makensis "packaging\cupbox.nsi"
} else {
    Write-Warning "NSIS not found: skipped the installer"
}

$stage = "dist\Cupbox-0.1.0-portable"
Remove-Item $stage -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory $stage | Out-Null
Copy-Item cupbox.exe, "*.dll", "assets\icon.ico", "packaging\NOTICE.txt", `
    "packaging\LICENSE-FFmpeg-LGPLv2.1.txt" $stage
Compress-Archive -Path "$stage\*" -DestinationPath "dist\Cupbox-0.1.0-portable.zip" -Force
Remove-Item $stage -Recurse -Force

Get-ChildItem dist | Select-Object Name, Length
