; Cupbox installer: per-user install, MP4 association, no admin rights.
Unicode true
!include "MUI2.nsh"
!include "FileFunc.nsh"

!define APPNAME    "Cupbox"
!define APPVER     "0.1.0"
!define PROGID     "Cupbox.Video"
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
!define MUI_FINISHPAGE_RUN_TEXT "现在打开“默认应用”设置"
!define MUI_FINISHPAGE_RUN_FUNCTION OpenDefaultApps
!define MUI_FINISHPAGE_TEXT "Cupbox 已安装。$\r$\n$\r$\nWindows 不允许安装程序代设默认播放器。请在“默认应用”里把 mp4、mkv 等视频格式指给 Cupbox；也可以右键任意视频 → 打开方式 → 始终使用。"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "English"


; Every extension Cupbox can play, offered in "Open with" and in the
; default apps settings.
!macro AssocExt EXT
  WriteRegStr HKCU "Software\Classes\${EXT}\OpenWithProgids" "${PROGID}" ""
  WriteRegStr HKCU "Software\${APPNAME}\Capabilities\FileAssociations" "${EXT}" "${PROGID}"
!macroend
!macro UnassocExt EXT
  DeleteRegValue HKCU "Software\Classes\${EXT}\OpenWithProgids" "${PROGID}"
  DeleteRegValue HKCU "Software\${APPNAME}\Capabilities\FileAssociations" "${EXT}"
!macroend

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
  File "..\zlib1.dll"
  File "..\libwinpthread-1.dll"
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

  ; The video types Cupbox handles
  WriteRegStr HKCU "Software\Classes\${PROGID}" "" "Cupbox 视频"
  WriteRegStr HKCU "Software\Classes\${PROGID}" "FriendlyTypeName" "Cupbox 视频"
  WriteRegStr HKCU "Software\Classes\${PROGID}\DefaultIcon" "" "$INSTDIR\cupbox.exe,0"
  WriteRegStr HKCU "Software\Classes\${PROGID}\shell\open\command" "" '"$INSTDIR\cupbox.exe" "%1"'

  ; What Settings > Default apps reads
  WriteRegStr HKCU "Software\${APPNAME}\Capabilities" "ApplicationName"        "${APPNAME}"
  WriteRegStr HKCU "Software\${APPNAME}\Capabilities" "ApplicationDescription" "极简视频播放器：双击就播，没有界面。"
  WriteRegStr HKCU "Software\RegisteredApplications" "${APPNAME}" "Software\${APPNAME}\Capabilities"

  !insertmacro AssocExt ".mp4"
  !insertmacro AssocExt ".m4v"
  !insertmacro AssocExt ".mkv"
  !insertmacro AssocExt ".webm"
  !insertmacro AssocExt ".avi"
  !insertmacro AssocExt ".mov"
  !insertmacro AssocExt ".flv"
  !insertmacro AssocExt ".ts"
  !insertmacro AssocExt ".m2ts"
  !insertmacro AssocExt ".mts"
  !insertmacro AssocExt ".mpg"
  !insertmacro AssocExt ".mpeg"
  !insertmacro AssocExt ".vob"
  !insertmacro AssocExt ".wmv"
  !insertmacro AssocExt ".asf"
  !insertmacro AssocExt ".ogv"

  ; An earlier version used one ProgID per type: clear it away
  DeleteRegKey HKCU "Software\Classes\Cupbox.mp4"
  DeleteRegValue HKCU "Software\Classes\.mp4\OpenWithProgids" "Cupbox.mp4"


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
  Delete "$INSTDIR\zlib1.dll"
  Delete "$INSTDIR\libwinpthread-1.dll"
  Delete "$INSTDIR\icon.ico"
  Delete "$INSTDIR\NOTICE.txt"
  Delete "$INSTDIR\LICENSE-FFmpeg-LGPLv2.1.txt"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\${APPNAME}\${APPNAME}.lnk"
  RMDir "$SMPROGRAMS\${APPNAME}"
  Delete "$DESKTOP\${APPNAME}.lnk"

  DeleteRegKey HKCU "Software\Classes\${PROGID}"
  !insertmacro UnassocExt ".mp4"
  !insertmacro UnassocExt ".m4v"
  !insertmacro UnassocExt ".mkv"
  !insertmacro UnassocExt ".webm"
  !insertmacro UnassocExt ".avi"
  !insertmacro UnassocExt ".mov"
  !insertmacro UnassocExt ".flv"
  !insertmacro UnassocExt ".ts"
  !insertmacro UnassocExt ".m2ts"
  !insertmacro UnassocExt ".mts"
  !insertmacro UnassocExt ".mpg"
  !insertmacro UnassocExt ".mpeg"
  !insertmacro UnassocExt ".vob"
  !insertmacro UnassocExt ".wmv"
  !insertmacro UnassocExt ".asf"
  !insertmacro UnassocExt ".ogv"
  DeleteRegKey HKCU "Software\Classes\Applications\cupbox.exe"
  DeleteRegKey HKCU "Software\${APPNAME}"
  DeleteRegValue HKCU "Software\RegisteredApplications" "${APPNAME}"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPNAME}"
SectionEnd
