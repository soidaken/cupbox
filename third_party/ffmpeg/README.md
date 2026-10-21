# FFmpeg for Cupbox

The Windows build of FFmpeg 8.0.3 that Cupbox links against (LGPL v2.1+,
shared libraries, no GPL parts). It is committed here so that a checkout
builds without a separate toolchain step.

- `include/`, `lib/*.dll.a` — what cgo compiles and links against
  (`decoder/*.go` points at `.tools/ffmpeg-min`, and `packaging/build.ps1`
  copies this directory there when it is missing).
- `bin/*.dll` — what goes next to `cupbox.exe`.

Built from FFmpeg `release/8.0` with MSYS2 mingw64 (gcc 14.2.0) and:

```
--arch=x86_64 --target-os=mingw32 --disable-x86asm --disable-everything
--disable-avdevice --disable-avfilter --disable-network --disable-doc
--disable-debug --disable-autodetect --enable-zlib
--enable-demuxer=mov,matroska,avi,flv,mpegts,mpegps,asf,ogg,wav,mp3,flac,aac
--enable-decoder=h264,hevc,av1,vp9,vp8,mpeg4,mpeg2video,mpeg1video,vc1,
  wmv3,wmv2,wmv1,msmpeg4v3,msmpeg4v2,flv,mjpeg,h263,vp6f,vp6a,aac,mp3,opus,
  flac,ac3,eac3,alac,vorbis,wmav1,wmav2,dca,truehd,mp2,pcm_s16le,pcm_s24le,
  ape,wavpack,amr_nb,amr_wb,speex,ass,subrip,movtext,webvtt,pgssub,dvdsub,dvbsub
--enable-parser=h264,hevc,av1,vp9,vp8,mpeg4video,mpegvideo,vc1,opus,flac,
  mpegaudio,ac3,dca,aac
--enable-protocol=file --enable-shared --disable-static
```

`--disable-x86asm` because the build machine had no nasm; it costs some
decode speed and nothing else.

## Licence

FFmpeg is licensed under the LGPL v2.1 or later. The full text ships in
`packaging/LICENSE-FFmpeg-LGPLv2.1.txt` and in every installer, together
with `packaging/NOTICE.txt`. The libraries are used unmodified and
dynamically linked, so they can be replaced by the user.

FFmpeg source: <https://ffmpeg.org/download.html> (branch `release/8.0`).
Build configuration above is everything needed to rebuild an ABI-compatible
set of libraries.
