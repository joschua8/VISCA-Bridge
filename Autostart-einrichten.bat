@echo off
rem Legt eine Verknuepfung zur fensterlosen Version in den Autostart-Ordner
rem des angemeldeten Benutzers und startet die Bridge sofort.
set "DIR=%~dp0"
powershell -NoProfile -Command ^
  "$s=(New-Object -ComObject WScript.Shell).CreateShortcut([Environment]::GetFolderPath('Startup')+'\VISCA-Bridge.lnk');" ^
  "$s.TargetPath='%DIR%visca-bridge-ohne-fenster.exe';$s.WorkingDirectory='%DIR%';$s.Save()"
start "" "%DIR%visca-bridge-ohne-fenster.exe"
echo.
echo VISCA-Bridge startet jetzt und kuenftig automatisch bei der Anmeldung.
echo Status im Browser: http://localhost:8099
pause
