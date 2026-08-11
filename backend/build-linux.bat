@echo off
setlocal

REM ============================================================
REM  WebMap backend - Cross-compile to Linux amd64
REM  Usage: double-click to run
REM  Output: l4d2_webmap
REM ============================================================

cd /d "%~dp0"

echo.
echo  [WebMap] Cross-compiling for Linux amd64 ...
echo.

set GOOS=linux
set GOARCH=amd64
set CGO_ENABLED=0

go build -o l4d2_webmap .

if errorlevel 1 (
    echo.
    echo  [ERROR] Build failed. Check the errors above.
    echo.
    pause
    exit /b 1
)

echo.
echo  [OK] Build succeeded!
echo.
echo  Output : %cd%\l4d2_webmap
echo  Target : Linux amd64 (GOOS=linux GOARCH=amd64 CGO_ENABLED=0)
echo.
echo  Deploy :
echo    chmod +x l4d2_webmap
echo    ./l4d2_webmap init
echo    ./l4d2_webmap serve -c data/config.json
echo.

set GOOS=
set GOARCH=
set CGO_ENABLED=

pause
endlocal
