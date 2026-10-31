@echo off
REM UptimeX local dev stack — starts the monitor API (:8010) and the
REM dashboard (:7777). Double-click this file or run it from a terminal.
REM Stop both with Ctrl+C in each window (or close the windows).
REM The SQLite database lives in .\dev-data\ (git-ignored) so it survives
REM reboots — do NOT put it back in %TEMP%, Windows wipes that.

setlocal
cd /d "%~dp0"
if not exist dev-data mkdir dev-data

echo [1/2] Building + starting the monitor API on http://localhost:8010 ...
start "UptimeX monitor" cmd /k "cd /d "%~dp0" && go build -o "%TEMP%\uptimex-monitor.exe" ./cmd/monitor && set "DB_DRIVER=sqlite" && set "SQLITE_PATH=%~dp0dev-data\uptimex.db" && set "HTTP_ADDR=:8010" && set "SAAS_MODE=true" && set "ALLOW_PRIVATE_TARGETS=true" && set "SEED_DEMO_ENDPOINTS=true" && set "PUBLIC_URL=http://localhost:7777" && "%TEMP%\uptimex-monitor.exe""

timeout /t 3 /nobreak >nul

echo [2/2] Starting the dashboard on http://localhost:7777 ...
start "UptimeX dashboard" cmd /k "cd /d "%~dp0dashboard" && set "VITE_MONITOR_URL=http://localhost:8010" && node .\node_modules\vite\bin\vite.js --port 7777 --strictPort"

echo.
echo   Site    : http://localhost:7777
echo   Login   : create a workspace via Start free (fresh database), or
echo              demo@acme.io / password123 after running once with the old temp DB
echo   Demo status page : http://localhost:7777/status/acme-corp (auto-seeded)
echo   API     : http://localhost:8010/health
echo.
echo Two windows opened. Close them to stop the stack.
endlocal
