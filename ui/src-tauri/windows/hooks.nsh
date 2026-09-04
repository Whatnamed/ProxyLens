!macro NSIS_HOOK_PREINSTALL
  IfFileExists "$INSTDIR\proxylens-supervisor.exe" 0 preinstall_end
  ExecWait '"$INSTDIR\proxylens-supervisor.exe" install unregister' $0
  IntCmp $0 0 preinstall_stop preinstall_fail preinstall_fail
preinstall_fail:
  Abort "ProxyLens could not disable the previous background owner safely."
preinstall_stop:
  IfFileExists "$INSTDIR\proxylens-supervisor.exe" 0 preinstall_end
  ExecWait '"$INSTDIR\proxylens-supervisor.exe" control stop --wait 15s' $0
  IntCmp $0 0 preinstall_end preinstall_stop_fail preinstall_stop_fail
preinstall_stop_fail:
  Abort "ProxyLens could not stop the previous background owner safely."
preinstall_end:
!macroend

!macro NSIS_HOOK_POSTINSTALL
  IfFileExists "$INSTDIR\proxylens-supervisor.exe" 0 postinstall_end
  ExecWait '"$INSTDIR\proxylens-supervisor.exe" install ensure-owner' $0
  IntCmp $0 0 postinstall_end postinstall_fail postinstall_fail
postinstall_fail:
  Abort "ProxyLens could not reconcile the installed background owner."
postinstall_end:
!macroend

!macro NSIS_HOOK_PREUNINSTALL
  IfFileExists "$INSTDIR\proxylens-supervisor.exe" 0 preuninstall_end
  ExecWait '"$INSTDIR\proxylens-supervisor.exe" install unregister' $0
  IntCmp $0 0 preuninstall_stop preuninstall_fail preuninstall_fail
preuninstall_fail:
  Abort "ProxyLens could not disable the background owner before uninstall."
preuninstall_stop:
  ExecWait '"$INSTDIR\proxylens-supervisor.exe" control stop --wait 15s' $0
  IntCmp $0 0 preuninstall_end preuninstall_stop_fail preuninstall_stop_fail
preuninstall_stop_fail:
  Abort "ProxyLens could not stop the background owner before uninstall."
preuninstall_end:
!macroend
