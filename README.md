# Cupbox

双击视频就播的 Windows 播放器：无边框、无按钮、没有界面，只有画面和一条会在需要时出现的进度条。

- 无边框窗口，按视频原始分辨率弹出（超出屏幕时等比缩小），圆角、无系统细边框
- 进度条浮层：鼠标移动时出现，静止 2 秒淡出；显示时间、帧率、原始分辨率、视频/音频码率
- 键盘：`空格` 播放/暂停，`←` `→` 跳 2 秒，`A` `D` 上一帧/下一帧（自动暂停），滚轮调音量，双击全屏，`Esc` 退出
- 播放结束：最后一帧模糊作背景，提示「播放结束，把视频拖到这里」「按空格键重新播放」
- 字体渲染的 ASS/SRT 字幕（外挂 `.srt` + 容器内嵌 ass/subrip/movtext/webvtt/pgs/dvd/dvb 字幕轨）
- 拖动窗口直接跟手，拖入文件立即切换，纯音频文件静默退出
- 安装包注册 16 种扩展的「打开方式」，按用户安装，不需要管理员权限

Win32 窗口、D3D11 渲染和 WASAPI 音频来自 [mygo](https://github.com/egoist/mygo)；解码用 FFmpeg 的最小共享构建（见 [third_party/ffmpeg](third_party/ffmpeg/README.md)）。

## 构建

需要 Go 1.27+，以及构建安装包时的 NSIS（`C:\Program Files (x86)\NSIS`）。FFmpeg 的头文件、导入库和 DLL 已经在 `third_party/`，不需要另外装。

```powershell
powershell packaging\build.ps1     # cupbox.exe + dist\Cupbox-Setup-0.1.0.exe + dist\Cupbox-0.1.0-portable.zip
go build -o cupbox.exe .           # 只出主程序（先跑一次 build.ps1 准备好 .tools\ffmpeg-min）
```

`packaging/build.ps1` 会把 `third_party/ffmpeg` 复制到 `.tools/ffmpeg-min`（cgo 的固定路径），并把运行时 DLL 拷到仓库根目录——测试和直接运行都靠根目录的 DLL。

## 测试

```powershell
$env:PATH="$PWD;$env:PATH"; go test ./...
```

`PATH` 里的仓库根目录是给测试进程找 FFmpeg DLL 用的。测试用的视频是 `testdata/media` 里几千字节的合成片段（h264/hevc/mpeg2/mpeg4/vp8/wmv/flv/mp4/mkv/ts/avi/webm，含带音轨、VFR、内嵌字幕几种），由 [testdata/media/generate.ps1](testdata/media/generate.ps1) 生成；新增格式时跑一次即可。

## CI

`.github/workflows/build.yml`：每次 push 和 PR 在 `windows-latest` 上构建、跑测试，产物作为 artifact 上传；打 `v*` 标签时自动发 Release，附上安装包和便携版 zip。

## 启动日志

程序默认在 `%LOCALAPPDATA%\Cupbox\logs` 保留最近 20 次启动的分段日志（打开视频、首帧解码、窗口创建、首帧上屏、音频时钟接管等），每份上限 64KB。定位「第一次启动慢」这类问题用得上；设 `CUPBOX_LOG=<路径>` 可换成完整调试日志。

## 目录

```
main.go              窗口、绘制、交互、播放循环
startup.go           启动分段日志
decoder/             FFmpeg 封装：视频帧、音频 PCM、字幕轨、容器元数据
subtitle.go          SRT/ASS 解析与渲染
input.go             输入状态
packaging/           NSIS 脚本与打包脚本
testdata/media/      测试素材与生成脚本
third_party/         预编译 FFmpeg（LGPL）与 MSYS2 运行时 DLL
docs/DESIGN.md       设计说明与实测记录
```

## 许可

Cupbox 自身尚未附许可证文件（如需开源授权请先添加）。FFmpeg 以 LGPL v2.1+ 动态链接使用，许可证与声明随安装包发布：`packaging/LICENSE-FFmpeg-LGPLv2.1.txt`、`packaging/NOTICE.txt`。
