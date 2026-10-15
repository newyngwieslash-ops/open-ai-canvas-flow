@echo off
setlocal
cd /d "%~dp0"
rem Yingce local secondary-dev restart.
rem Frontend 5178 (fallback 15178), backend 8178 (does not use 3000/8080).
rem See docs/content/docs/backend/local-development.mdx
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\restart-local.ps1" %*
exit /b %ERRORLEVEL%

