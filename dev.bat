@echo off
REM UptimeX local dev stack — starts the monitor API (:8010) and the
REM dashboard (:7777). Double-click this file or run it from a terminal.
REM Stop both with Ctrl+C in each window (or close the windows).

setlocal
cd /d "%~dp0"

echo [1/2] Building + starting the monitor API on http://localhost:8010 ...
start "UptimeX monitor" cmd /k "cd /d "%~dp0" && go build -o "%TEMP%\uptimex-monitor.exe" ./cmd/monitor && DB_DRIVER=sqlite SQLITE_PATH=%TEMP%\uptimex-saas.db HTTP_ADDR=:8010 SAAS_MODE=true ALLOW_PRIVATE_TARGETS=true SEED_DEMO_ENDPOINTS=false "%TEMP%\uptimex-monitor.exe""

timeout /t 3 /nobreak >nul

echo [2/2] Starting the dashboard on http://localhost:7777 ...
start "UptimeX dashboard" cmd /k "cd /d "%~dp0dashboard" && set VITE_MONITOR_URL=http://localhost:8010 && node_modules\.bin\vite --port 7777 --strictPort"

echo.
echo   Site    : http://localhost:7777
echo   Login   : demo@acme.io / password123 (or create a workspace)
echo   API     : http://localhost:8010/health
echo.
echo Two windows opened. Close them to stop the stack.
endlocal
