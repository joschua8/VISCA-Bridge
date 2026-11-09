@echo off
rem Entfernt die Bridge aus dem Autostart und beendet sie.
del "%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\VISCA-Bridge.lnk" 2>nul
taskkill /im visca-bridge-ohne-fenster.exe /f 2>nul
echo VISCA-Bridge beendet und aus dem Autostart entfernt.
pause
