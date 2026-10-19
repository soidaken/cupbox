# Builds cupbox.exe, the Windows installer and the portable zip.
# Needs: Go 1.27+, the FFmpeg build in .tools/ffmpeg-min, MSYS2 at C:\msys64
# (for zlib1.dll and libwinpthread-1.dll), NSIS at its default path.
$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root

# Runtime DLLs: FFmpeg, plus the two it needs that Windows does not have.
$ffmpeg = ".tools\ffmpeg-min\bin"
if (Test-Path $ffmpeg) {
    Copy-Item "$ffmpeg\*.dll" . -Force
}
$deps = @{
    "zlib1.dll"           = "C:\msys64\mingw64\bin\zlib1.dll"
    "libwinpthread-1.dll" = "C:\msys64\mingw64\bin\libwinpthread-1.dll"
}
foreach ($name in $deps.Keys) {
    if (-not (Test-Path $name)) {
        if (Test-Path $deps[$name]) {
            Copy-Item $deps[$name] $name
        } else {
            throw "missing runtime DLL: $($deps[$name])"
        }
    }
}

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
