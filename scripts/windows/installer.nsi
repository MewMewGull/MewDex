; Current-user installer. Startup is an explicit choice, shared with the Go
; manager and desktop. No cmd/PowerShell startup script or second SSH service.
Unicode true
ManifestDPIAware true
RequestExecutionLevel user
!include "MUI2.nsh"
!include "nsDialogs.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"

!ifndef PAYLOAD
  !error "Pass /DPAYLOAD=<prepared desktop folder>"
!endif
!ifndef OUTPUT
  !error "Pass /DOUTPUT=<installer.exe>"
!endif
!ifndef BOOTSTRAPPER
  !error "Pass /DBOOTSTRAPPER=<Microsoft-signed WebView2 bootstrapper>"
!endif
!ifndef MANIFEST
  !error "Pass /DMANIFEST=<uninstall-files.nsh>"
!endif
!ifndef APPVERSION
  !error "Pass /DAPPVERSION=<app SemVer>"
!endif
Name "MewDex"
OutFile "${OUTPUT}"
InstallDir "$LOCALAPPDATA\Programs\MewDex"
InstallDirRegKey HKCU "Software\MewDex" "InstallDir"
SetCompressor /SOLID lzma
ShowInstDetails show
ShowUninstDetails show
Var AutoStart
Var StartupCheckbox
Var Dialog
Var Parameters
Var ExplicitStartup
Var UpdatePID

!define MUI_ABORTWARNING
!define MUI_CUSTOMFUNCTION_ABORT ReopenWorkbench
!define MUI_PAGE_CUSTOMFUNCTION_PRE SkipUpdatePage
!insertmacro MUI_PAGE_WELCOME
!define MUI_PAGE_CUSTOMFUNCTION_PRE SkipUpdatePage
!insertmacro MUI_PAGE_DIRECTORY
Page custom StartupPage StartupLeave
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\mewdex-client.exe"
!define MUI_FINISHPAGE_RUN_TEXT "打开 MewDex 工作台"
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "English"

Function .onInit
  SetShellVarContext current
  ${GetParameters} $Parameters
  ${GetOptions} $Parameters "/UPDATEPID=" $UpdatePID
  ${If} $UpdatePID != ""
    ; Wait for the invoking workbench to release its executable and WebView.
    System::Call 'kernel32::OpenProcess(i 0x00100000, i 0, i $UpdatePID) p .r0'
    ${If} $0 != 0
      System::Call 'kernel32::WaitForSingleObject(p r0, i 30000) i .r1'
      System::Call 'kernel32::CloseHandle(p r0)'
      ${If} $1 != 0
        MessageBox MB_OK|MB_ICONSTOP "工作台尚未退出，请关闭后重新运行更新包。" /SD IDOK
        Abort
      ${EndIf}
    ${EndIf}
  ${EndIf}
  StrCpy $AutoStart "false"
  InitPluginsDir
  SetOutPath "$PLUGINSDIR"
  File /oname=mewdex-node.exe "${PAYLOAD}\mewdex-node.exe"
  ClearErrors
  ExecWait '"$PLUGINSDIR\mewdex-node.exe" install -root "$LOCALAPPDATA\app.mewdex.client" -app-config "$APPDATA\app.mewdex.client" -inspect-output "$PLUGINSDIR\startup.ini"' $0
  ${If} ${Errors}
    MessageBox MB_OK|MB_ICONSTOP "无法读取已有的 Hub 自动启动设置，安装已停止。" /SD IDOK
    Call ReopenWorkbench
    Abort
  ${EndIf}
  ${If} $0 != 0
    MessageBox MB_OK|MB_ICONSTOP "已有的 Hub 设置需要核对，请在工作台中处理后再安装。" /SD IDOK
    Call ReopenWorkbench
    Abort
  ${EndIf}
  ReadINIStr $AutoStart "$PLUGINSDIR\startup.ini" "MewDex" "AutoStart"
  ClearErrors
  ${GetOptions} $Parameters "/AUTOSTART=" $ExplicitStartup
  ${IfNot} ${Errors}
    ${If} $ExplicitStartup != "true"
    ${AndIf} $ExplicitStartup != "false"
      MessageBox MB_OK|MB_ICONSTOP "/AUTOSTART 只能使用 true 或 false。" /SD IDOK
      Call ReopenWorkbench
      Abort
    ${EndIf}
    StrCpy $AutoStart $ExplicitStartup
  ${EndIf}
  ; Check before copying application files, including manual installer upgrades.
  ClearErrors
  ExecWait '"$PLUGINSDIR\mewdex-node.exe" install -root "$LOCALAPPDATA\app.mewdex.client" -app-config "$APPDATA\app.mewdex.client" -prepare-update' $0
  ${If} ${Errors}
    MessageBox MB_OK|MB_ICONSTOP "无法检查本机任务，更新已停止。请重新打开工作台后重试。" /SD IDOK
    Call ReopenWorkbench
    Abort
  ${EndIf}
  ${If} $0 != 0
    MessageBox MB_OK|MB_ICONSTOP "本机存在执行中或待确认任务，或服务状态需要核对。请在工作台处理后重新安装。" /SD IDOK
    Call ReopenWorkbench
    Abort
  ${EndIf}
FunctionEnd

Function SkipUpdatePage
  ${If} $UpdatePID != ""
    Abort
  ${EndIf}
FunctionEnd

Function ReopenWorkbench
  ${If} $UpdatePID != ""
    Exec '"$INSTDIR\mewdex-client.exe"'
  ${EndIf}
FunctionEnd

Function .onInstSuccess
  ${If} $UpdatePID != ""
    Call ReopenWorkbench
    Quit
  ${EndIf}
FunctionEnd

Function .onInstFailed
  Call ReopenWorkbench
FunctionEnd

Function StartupPage
  Call SkipUpdatePage
  !insertmacro MUI_HEADER_TEXT "Hub 启动设置" "选择是否在登录 Windows 后自动启动 MewDex Hub。"
  nsDialogs::Create 1018
  Pop $Dialog
  ${If} $Dialog == error
    Abort
  ${EndIf}
  ${NSD_CreateLabel} 0 0 100% 36u "MewDex Hub 是后台服务，负责设备连接和可选的公网反向代理。登录时静默启动到系统托盘。"
  Pop $0
  ${NSD_CreateCheckbox} 0 46u 100% 20u "开机自启动 MewDex Hub（登录当前 Windows 账户后）"
  Pop $StartupCheckbox
  ${If} $AutoStart == "true"
    ${NSD_Check} $StartupCheckbox
  ${EndIf}
  ${NSD_CreateLabel} 0 80u 100% 60u "首次安装默认关闭；升级保留当前设置。新设备完成首次配置后生效。之后可在桌面端的“本机设备 → 启动设置”中修改。升级会接管已有公网代理，并更新空闲的 Hub；有未完成任务时暂停升级。"
  Pop $0
  nsDialogs::Show
FunctionEnd

Function StartupLeave
  ${NSD_GetState} $StartupCheckbox $0
  StrCpy $AutoStart "false"
  ${If} $0 == ${BST_CHECKED}
    StrCpy $AutoStart "true"
  ${EndIf}
FunctionEnd

Function EnsureWebView2
  ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${If} $0 == ""
    ReadRegStr $0 HKLM "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${EndIf}
  ${If} $0 == ""
    SetRegView 64
    ReadRegStr $0 HKLM "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
    SetRegView 32
  ${EndIf}
  ${If} $0 == ""
    DetailPrint "正在安装 Microsoft Edge WebView2 Runtime…"
    SetOutPath "$PLUGINSDIR"
    File /oname=WebView2Setup.exe "${BOOTSTRAPPER}"
    ClearErrors
    ExecWait '"$PLUGINSDIR\WebView2Setup.exe" /silent /install' $0
    ${If} ${Errors}
      MessageBox MB_OK|MB_ICONSTOP "无法启动 WebView2 安装，请安装 Microsoft Edge WebView2 Runtime 后重试。" /SD IDOK
      Abort
    ${EndIf}
    ${If} $0 != 0
    ${AndIf} $0 != 3010
      MessageBox MB_OK|MB_ICONSTOP "WebView2 安装未完成，请检查网络或安装 Microsoft Edge WebView2 Runtime 后重试。" /SD IDOK
      Abort
    ${EndIf}
  ${EndIf}
FunctionEnd

Section "MewDex" Main
  SetShellVarContext current
  Call EnsureWebView2
  SetOutPath "$INSTDIR"
  File /r "${PAYLOAD}\*"
  ClearErrors
  ExecWait '"$INSTDIR\mewdex-node.exe" install -root "$LOCALAPPDATA\app.mewdex.client" -app-config "$APPDATA\app.mewdex.client" -workbench "$INSTDIR\mewdex-client.exe" -autostart=$AutoStart' $0
  ${If} ${Errors}
    MessageBox MB_OK|MB_ICONSTOP "无法保存 Hub 启动设置。安装未完成，请重试。" /SD IDOK
    Abort
  ${EndIf}
  ${If} $0 != 0
    MessageBox MB_OK|MB_ICONSTOP "Hub 启动设置保存失败，安装未完成。请在工作台中核对已有配置后重试。" /SD IDOK
    Abort
  ${EndIf}
  WriteRegStr HKCU "Software\MewDex" "InstallDir" "$INSTDIR"
  CreateDirectory "$SMPROGRAMS\MewDex"
  CreateShortcut "$SMPROGRAMS\MewDex\MewDex.lnk" "$INSTDIR\mewdex-client.exe"
  CreateShortcut "$DESKTOP\MewDex.lnk" "$INSTDIR\mewdex-client.exe"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\MewDex" "DisplayName" "MewDex"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\MewDex" "DisplayVersion" "${APPVERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\MewDex" "UninstallString" '$\"$INSTDIR\Uninstall.exe$\"'
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\MewDex" "InstallLocation" "$INSTDIR"
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\MewDex" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\MewDex" "NoRepair" 1
SectionEnd

Section "Uninstall"
  SetShellVarContext current
  ClearErrors
  ExecWait '"$INSTDIR\mewdex-node.exe" install -root "$LOCALAPPDATA\app.mewdex.client" -app-config "$APPDATA\app.mewdex.client" -uninstall' $0
  ${If} ${Errors}
    MessageBox MB_OK|MB_ICONSTOP "无法停止 Hub；为保留可管理的入口，卸载已停止。" /SD IDOK
    Abort
  ${EndIf}
  ${If} $0 != 0
    MessageBox MB_OK|MB_ICONSTOP "Hub 停止或启动项移除尚未确认，请在工作台中核对后重试卸载。" /SD IDOK
    Abort
  ${EndIf}
  ; Remove only files installed by this package; preserve user data and any
  ; unrelated files placed in the installation directory.
  !include "${MANIFEST}"
  Delete "$SMPROGRAMS\MewDex\MewDex.lnk"
  RMDir "$SMPROGRAMS\MewDex"
  Delete "$DESKTOP\MewDex.lnk"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
  DeleteRegKey HKCU "Software\MewDex"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\MewDex"
SectionEnd
