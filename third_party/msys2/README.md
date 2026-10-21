# MSYS2 runtime DLLs

Two libraries FFmpeg's Windows build needs that Windows itself does not
have, taken unmodified from MSYS2 mingw64:

- `zlib1.dll` — zlib, needed by avformat for zlib-compressed subtitle
  tracks in Matroska.
- `libwinpthread-1.dll` — the winpthreads runtime of mingw-w64, needed by
  avutil for its threads.

`packaging/build.ps1` copies them next to `cupbox.exe`, preferring the
ones in this directory so that neither a local MSYS2 install nor MSYS2 on
a CI runner is required.

Licences: zlib (zlib licence), winpthreads (MIT / BSD-style, part of
mingw-w64).
