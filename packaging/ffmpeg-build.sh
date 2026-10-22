#!/bin/bash
# Rebuilds the minimal FFmpeg Cupbox ships, with D3D11VA hardware decoding,
# into .tools/ffmpeg-hw. The source is expected in .tools/ffmpeg-src (FFmpeg
# release/8.0); third_party/ffmpeg/README.md says what the build is for.
#
#   curl -LO https://ffmpeg.org/releases/ffmpeg-8.0.3.tar.xz
#   tar -xf ffmpeg-8.0.3.tar.xz -C .tools && mv .tools/ffmpeg-8.0.3 .tools/ffmpeg-src
#   C:\msys64\usr\bin\bash -lc "cd /path/to/cupbox && bash packaging/ffmpeg-build.sh"
#
# The result goes into third_party/ffmpeg (bin/*.dll, lib/*.dll.a, include/).
set -e
export PATH=/mingw64/bin:$PATH
cd "$(dirname "$0")/../.tools/ffmpeg-src"
make distclean >/dev/null 2>&1 || true
OUT="$(cd "$(dirname "$0")/.." && pwd)/.tools/ffmpeg-hw"
rm -rf "$OUT"
./configure --prefix=$OUT --arch=x86_64 --target-os=mingw32 --disable-x86asm \
  --disable-everything --disable-avdevice --disable-avfilter --disable-network \
  --disable-doc --disable-debug --disable-autodetect --enable-zlib \
  --enable-d3d11va \
  --enable-hwaccel=h264_d3d11va,h264_d3d11va2,hevc_d3d11va,hevc_d3d11va2,vp9_d3d11va,vp9_d3d11va2,av1_d3d11va,av1_d3d11va2,mpeg2_d3d11va,mpeg2_d3d11va2,vc1_d3d11va,vc1_d3d11va2,wmv3_d3d11va,wmv3_d3d11va2 \
  --enable-demuxer='mov,matroska,avi,flv,mpegts,mpegps,asf,ogg,wav,mp3,flac,aac' \
  --enable-decoder='h264,hevc,av1,vp9,vp8,mpeg4,mpeg2video,mpeg1video,vc1,wmv3,wmv2,wmv1,msmpeg4v3,msmpeg4v2,flv,mjpeg,h263,vp6f,vp6a,aac,mp3,opus,flac,ac3,eac3,alac,vorbis,wmav1,wmav2,dca,truehd,mp2,pcm_s16le,pcm_s24le,ape,wavpack,amr_nb,amr_wb,speex,ass,subrip,movtext,webvtt,pgssub,dvdsub,dvbsub' \
  --enable-parser='h264,hevc,av1,vp9,vp8,mpeg4video,mpegvideo,vc1,opus,flac,mpegaudio,ac3,dca,aac' \
  --enable-protocol=file --enable-shared --disable-static 2>&1 | tail -20
make -j"$(nproc)" 2>&1 | tail -5
make install 2>&1 | tail -3
echo "=== built ==="
ls -l "$OUT/bin"/*.dll | awk '{print $5, $9}'
echo "copy bin/*.dll, lib/*.dll.a and include/ into third_party/ffmpeg, then run packaging/build.ps1"
