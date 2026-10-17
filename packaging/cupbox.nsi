; Cupbox installer: per-user install, MP4 association, no admin rights.
Unicode true
!include "MUI2.nsh"
!include "FileFunc.nsh"

!define APPNAME    "Cupbox"
!define APPVER     "0.1.0"
!define PROGID     "Cupbox.mp4"
!define PUBLISHER  "Cupbox"

Name "${APPNAME}"
OutFile "..\dist\${APPNAME}-Setup-${APPVER}.exe"
InstallDir "$LOCALAPPDATA\Programs\${APPNAME}"
InstallDirRegKey HKCU "Software\${APPNAME}" "InstallDir"
RequestExecutionLevel user
SetCompressor /SOLID lzma

VIProductVersion "0.1.0.0"
VIAddVersionKey "ProductName"     "${APPNAME}"
VIAddVersionKey "FileDescription" "${APPNAME} 安装程序"
VIAddVersionKey "FileVersion"     "${APPVER}"
VIAddVersionKey "ProductVersion"  "${APPVER}"
VIAddVersionKey "LegalCopyright"  "${PUBLISHER}"

!define MUI_ICON   "..\assets\icon.ico"
!define MUI_UNICON "..\assets\icon.ico"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "打开 Windows 的“默认应用”设置，把 Cupbox 设为 .mp4 的默认播放器"
!define MUI_FINISHPAGE_RUN_FUNCTION OpenDefaultApps
!define MUI_FINISHPAGE_TEXT "Cupbox 已安装。$\r$\n$\r$\nWindows 10/11 不允许安装程序直接改默认播放器，需要你在“默认应用”里选一次 .mp4：$\r$\n找到 Cupbox，或在 .mp4 的“更改默认应用”里选它。$\r$\n$\r$\n也可以右键任意 mp4 → 打开方式 → 选择其他应用 → Cupbox → 始终使用。"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "English"

Function OpenDefaultApps
  ExecShell "open" "ms-settings:defaultapps"
FunctionEnd

Section "Cupbox" SecMain
  SetShellVarContext current
  SetOutPath "$INSTDIR"
  File "..\cupbox.exe"
  File "..\avcodec-62.dll"
  File "..\avformat-62.dll"
  File "..\avutil-60.dll"
  File "..\swscale-9.dll"
  File "..\swresample-6.dll"
  File "..\assets\icon.ico"
  File "..\packaging\NOTICE.txt"
  File "..\packaging\LICENSE-FFmpeg-LGPLv2.1.txt"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  WriteRegStr HKCU "Software\${APPNAME}" "InstallDir" "$INSTDIR"

  ; Programs and Features entry
  !define UNKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPNAME}"
  WriteRegStr HKCU "${UNKEY}" "DisplayName"     "${APPNAME}"
  WriteRegStr HKCU "${UNKEY}" "DisplayVersion"  "${APPVER}"
  WriteRegStr HKCU "${UNKEY}" "Publisher"       "${PUBLISHER}"
  WriteRegStr HKCU "${UNKEY}" "DisplayIcon"     "$INSTDIR\cupbox.exe,0"
  WriteRegStr HKCU "${UNKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNKEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "${UNKEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegDWORD HKCU "${UNKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNKEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKCU "${UNKEY}" "EstimatedSize" "$0"

  ; The player as a program, so it shows in "Open with" for any file
  WriteRegStr HKCU "Software\Classes\Applications\cupbox.exe" "FriendlyAppName" "${APPNAME}"
  WriteRegStr HKCU "Software\Classes\Applications\cupbox.exe\shell\open\command" "" '"$INSTDIR\cupbox.exe" "%1"'

  ; MP4 handler
  WriteRegStr HKCU "Software\Classes\${PROGID}" "" "Cupbox 视频"
  WriteRegStr HKCU "Software\Classes\${PROGID}" "FriendlyTypeName" "Cupbox 视频"
  WriteRegStr HKCU "Software\Classes\${PROGID}\DefaultIcon" "" "$INSTDIR\cupbox.exe,0"
  WriteRegStr HKCU "Software\Classes\${PROGID}\shell\open\command" "" '"$INSTDIR\cupbox.exe" "%1"'
  WriteRegStr HKCU "Software\Classes\.mp4\OpenWithProgids" "${PROGID}" ""

  ; What Settings > Default apps reads
  WriteRegStr HKCU "Software\${APPNAME}\Capabilities" "ApplicationName"        "${APPNAME}"
  WriteRegStr HKCU "Software\${APPNAME}\Capabilities" "ApplicationDescription" "极简视频播放器：双击就播，没有界面。"
  WriteRegStr HKCU "Software\${APPNAME}\Capabilities\FileAssociations" ".mp4" "${PROGID}"
  WriteRegStr HKCU "Software\RegisteredApplications" "${APPNAME}" "Software\${APPNAME}\Capabilities"

  CreateDirectory "$SMPROGRAMS\${APPNAME}"
  CreateShortcut "$SMPROGRAMS\${APPNAME}\${APPNAME}.lnk" "$INSTDIR\cupbox.exe"
  CreateShortcut "$DESKTOP\${APPNAME}.lnk" "$INSTDIR\cupbox.exe"
SectionEnd

Section "Uninstall"
  SetShellVarContext current
  Delete "$INSTDIR\cupbox.exe"
  Delete "$INSTDIR\avcodec-62.dll"
  Delete "$INSTDIR\avformat-62.dll"
  Delete "$INSTDIR\avutil-60.dll"
  Delete "$INSTDIR\swscale-9.dll"
  Delete "$INSTDIR\swresample-6.dll"
  Delete "$INSTDIR\icon.ico"
  Delete "$INSTDIR\NOTICE.txt"
  Delete "$INSTDIR\LICENSE-FFmpeg-LGPLv2.1.txt"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\${APPNAME}\${APPNAME}.lnk"
  RMDir "$SMPROGRAMS\${APPNAME}"
  Delete "$DESKTOP\${APPNAME}.lnk"

  DeleteRegKey HKCU "Software\Classes\${PROGID}"
  DeleteRegValue HKCU "Software\Classes\.mp4\OpenWithProgids" "${PROGID}"
  DeleteRegKey HKCU "Software\Classes\Applications\cupbox.exe"
  DeleteRegKey HKCU "Software\${APPNAME}"
  DeleteRegValue HKCU "Software\RegisteredApplications" "${APPNAME}"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPNAME}"
SectionEnd
