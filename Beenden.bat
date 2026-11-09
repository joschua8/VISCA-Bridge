@echo off
taskkill /im visca-bridge-ohne-fenster.exe /f 2>nul
taskkill /im visca-bridge.exe /f 2>nul
echo VISCA-Bridge beendet.
pause
