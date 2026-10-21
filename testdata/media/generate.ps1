# Regenerates the tiny fixtures the decoder tests use, with any FFmpeg on
# PATH. The files are committed, so this only runs when a format is added.
# The minimized FFmpeg Cupbox ships can decode every format made here.
$ErrorActionPreference = "Stop"
$out = $PSScriptRoot
Set-Location $out
$src = "testsrc2=size=320x180:rate=30:duration=2"
$quiet = @("-hide_banner", "-v", "error", "-y")

function New-Fixture($name, [string[]]$opts) {
    & ffmpeg @quiet @opts $name
    if ($LASTEXITCODE -ne 0) { throw "ffmpeg failed for $name" }
    $size = (Get-Item $name).Length
    "{0,-14} {1,8} bytes" -f $name, $size
}

# H.264 with sound: the picture and the sound the player runs on.
New-Fixture "h264_aac.mp4" @("-f", "lavfi", "-i", $src, "-f", "lavfi", "-i", "sine=frequency=440:duration=2",
    "-c:v", "libx264", "-preset", "ultrafast", "-b:v", "300k", "-pix_fmt", "yuv420p",
    "-c:a", "aac", "-b:a", "64k", "-shortest")

# A real 1080p file, for the resolution the window takes from it.
New-Fixture "h264_1080.mp4" @("-f", "lavfi", "-i", "testsrc2=size=1920x1080:rate=30:duration=1",
    "-c:v", "libx264", "-preset", "ultrafast", "-crf", "30", "-pix_fmt", "yuv420p")

New-Fixture "h264.mkv" @("-f", "lavfi", "-i", $src, "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p")

New-Fixture "hevc.mp4" @("-f", "lavfi", "-i", $src, "-c:v", "libx265", "-preset", "ultrafast", "-crf", "32",
    "-pix_fmt", "yuv420p", "-tag:v", "hvc1")

New-Fixture "mpeg2.ts" @("-f", "lavfi", "-i", $src, "-c:v", "mpeg2video", "-b:v", "500k", "-f", "mpegts")

New-Fixture "mpeg4.avi" @("-f", "lavfi", "-i", $src, "-c:v", "mpeg4", "-b:v", "300k")

New-Fixture "vp8.webm" @("-f", "lavfi", "-i", $src, "-c:v", "libvpx", "-b:v", "200k")

New-Fixture "wmv.wmv" @("-f", "lavfi", "-i", $src, "-c:v", "wmv2", "-b:v", "300k", "-pix_fmt", "yuv420p")

New-Fixture "flv.flv" @("-f", "lavfi", "-i", $src, "-c:v", "flv", "-b:v", "300k", "-pix_fmt", "yuv420p")

# Subtitles in their three container spellings.
$srt = Join-Path $env:TEMP "cupbox-fixture.srt"
$ass = Join-Path $env:TEMP "cupbox-fixture.ass"
Set-Content -Path $srt -Encoding utf8 -Value @"
1
00:00:00,000 --> 00:00:01,500
Cupbox fixture subtitle
"@
Set-Content -Path $ass -Encoding utf8 -Value @"
[Script Info]
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, Bold
Style: Default,Arial,24,&H00FFFFFF,0

[Events]
Format: Layer, Start, End, Style, Text
Dialogue: 0,0:00:00.00,0:00:01.50,Default,Cupbox fixture subtitle
"@

New-Fixture "movtext.mp4" @("-f", "lavfi", "-i", $src, "-i", $srt, "-c:v", "libx264", "-preset", "ultrafast",
    "-pix_fmt", "yuv420p", "-c:s", "mov_text")
New-Fixture "ass.mkv" @("-f", "lavfi", "-i", $src, "-i", $ass, "-c:v", "libx264", "-preset", "ultrafast",
    "-pix_fmt", "yuv420p", "-c:s", "ass")
New-Fixture "srt.mkv" @("-f", "lavfi", "-i", $src, "-i", $srt, "-c:v", "libx264", "-preset", "ultrafast",
    "-pix_fmt", "yuv420p", "-c:s", "srt")

Remove-Item $srt, $ass -ErrorAction SilentlyContinue
