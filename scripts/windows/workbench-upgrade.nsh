!ifndef MEWDEX_WORKBENCH_UPGRADE_INCLUDED
!define MEWDEX_WORKBENCH_UPGRADE_INCLUDED

!include "LogicLib.nsh"
!include "Win\RestartManager.nsh"

Var WorkbenchWasRunning

; Opening with write access detects both running images and other file locks
; without truncating or modifying an existing installation.
!macro MewDexCheckInstalledBinary name
  ${If} $0 == 0
  ${AndIf} ${FileExists} "$INSTDIR\${name}"
    System::Call 'kernel32::CreateFileW(w "$INSTDIR\${name}", i 0x40000000, i 7, p 0, i 3, i 0, p 0) p.r1 ?e'
    Pop $2
    ${If} $1 == -1
      StrCpy $0 $2
    ${Else}
      System::Call 'kernel32::CloseHandle(p r1)'
    ${EndIf}
  ${EndIf}
!macroend

Function CloseInstalledWorkbench
  retry_close:
  StrCpy $0 0
  !insertmacro MewDexCheckInstalledBinary "mewdex-client.exe"
  !insertmacro MewDexCheckInstalledBinary "mewdex-input-bridge.exe"
  ${If} $0 == 0
    Return
  ${EndIf}

  !insertmacro RestartManager_StartSession $3
  ${If} $3 != ""
    !insertmacro RestartManager_RegisterFile $3 "$INSTDIR\mewdex-client.exe"
    ${If} $0 == 0
      !insertmacro RestartManager_RegisterFile $3 "$INSTDIR\mewdex-input-bridge.exe"
    ${EndIf}
    ${If} $0 == 0
      System::Call 'rstrtmgr::RmGetList(i r3, *i.r4, *i0, p 0, *i.r5) i.r0'
      ${If} $0 == 0
      ${OrIf} $0 == 234
        ${If} $4 > 0
        ${AndIf} $5 == 0
          MessageBox MB_YESNO|MB_ICONQUESTION|MB_DEFBUTTON2 "检测到旧版本 MewDex 正在运行。$\r$\n请确认当前进行中的任务可以中断，并保存尚未发送的输入。$\r$\n是否现在终止旧进程并继续安装？$\r$\n$\r$\n选择“是”后关闭旧版本；选择“否”取消本次安装。" /SD IDNO IDYES close_confirmed
          !insertmacro RestartManager_EndSession $3
          StrCpy $UpdatePID ""
          StrCpy $WorkbenchWasRunning "false"
          SetErrorLevel 1223
          Abort
          close_confirmed:
          StrCpy $WorkbenchWasRunning "true"
          DetailPrint "正在退出旧版本 MewDex…"
          ; Ask the applications using these exact installed files to exit.
          ; Flags 0 preserves their opportunity to finish or refuse shutdown.
          System::Call 'rstrtmgr::RmShutdown(i r3, i 0, p 0) i.r0'
        ${EndIf}
      ${EndIf}
    ${EndIf}
    !insertmacro RestartManager_EndSession $3
  ${EndIf}

  ; The APP-owned channel bridge may take a moment to finish after pipe EOF.
  StrCpy $4 0
  wait_for_files:
  StrCpy $0 0
  !insertmacro MewDexCheckInstalledBinary "mewdex-client.exe"
  !insertmacro MewDexCheckInstalledBinary "mewdex-input-bridge.exe"
  ${If} $0 == 0
    DetailPrint "旧版本已退出，开始安装。"
    Return
  ${EndIf}
  ${If} $4 < 200
    Sleep 50
    IntOp $4 $4 + 1
    Goto wait_for_files
  ${EndIf}

  MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "旧版本仍在运行，或安装目录不可写（错误 $0）。$\r$\n请从系统托盘退出 MewDex，并确认目录权限后重试。$\r$\n$INSTDIR" /SD IDCANCEL IDRETRY retry_close
  SetErrorLevel 1
  Abort
FunctionEnd

!endif
