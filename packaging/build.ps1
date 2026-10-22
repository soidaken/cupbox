# Builds cupbox.exe, the Windows installer and the portable zip.
# Needs: Go 1.27+, NSIS at its default path, and on a fresh checkout nothing
# else: the FFmpeg build and the MSYS2 DLLs are in third_party.
$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root

# FFmpeg, where cgo expects it. A local .tools\ffmpeg-min built by hand wins.
if (-not (Test-Path ".tools\ffmpeg-min\include")) {
    New-Item -ItemType Directory ".tools" -Force | Out-Null
    Copy-Item "third_party\ffmpeg" ".tools\ffmpeg-min" -Recurse -Force
}

# Runtime DLLs: FFmpeg, plus the two it needs that Windows does not have.
$ffmpeg = ".tools\ffmpeg-min\bin"
if (Test-Path $ffmpeg) {
    Copy-Item "$ffmpeg\*.dll" . -Force
}
$deps = @{
    "zlib1.dll"           = @("C:\msys64\mingw64\bin\zlib1.dll", "third_party\msys2\zlib1.dll")
    "libwinpthread-1.dll" = @("C:\msys64\mingw64\bin\libwinpthread-1.dll", "third_party\msys2\libwinpthread-1.dll")
}
foreach ($name in $deps.Keys) {
    if (Test-Path $name) { continue }
    $from = $deps[$name] | Where-Object { Test-Path $_ } | Select-Object -First 1
    if ($from) {
        Copy-Item $from $name
    } else {
        throw "missing runtime DLL: $name"
    }
}

# -s -w keeps the symbol table and debug info out: a smaller file for
# Windows and any antivirus to read, and for a slower disk.
go build -ldflags="-s -w -H windowsgui" -o cupbox.exe .

$makensis = "C:\Program Files (x86)\NSIS\makensis.exe"
if (Test-Path $makensis) {
    New-Item -ItemType Directory "dist" -Force | Out-Null
    & $makensis "packaging\cupbox.nsi"
    if ($LASTEXITCODE -ne 0) { throw "makensis failed with $LASTEXITCODE" }
} else {
    Write-Warning "NSIS not found: skipped the installer"
}

$stage = "dist\Cupbox-0.2.0-portable"
Remove-Item $stage -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory $stage | Out-Null
Copy-Item cupbox.exe, "*.dll", "assets\icon.ico", "packaging\NOTICE.txt", `
    "packaging\LICENSE-FFmpeg-LGPLv2.1.txt" $stage
Compress-Archive -Path "$stage\*" -DestinationPath "dist\Cupbox-0.2.0-portable.zip" -Force
Remove-Item $stage -Recurse -Force

Get-ChildItem dist | Select-Object Name, Length
