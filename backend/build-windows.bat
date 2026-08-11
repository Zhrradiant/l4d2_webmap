@echo off
setlocal

REM ============================================================
REM  WebMap backend - Build Windows amd64
REM  Usage: double-click to run
REM  Output: webmap.exe
REM ============================================================

cd /d "%~dp0"

echo.
echo  [WebMap] Building for Windows amd64 ...
echo.

set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0

go build -o webmap.exe .

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
echo  Output : %cd%\webmap.exe
echo  Target : Windows amd64 (GOOS=windows GOARCH=amd64 CGO_ENABLED=0)
echo.
echo  Deploy :
echo    Double-click webmap.exe to start, or run: webmap.exe serve -c data/config.json
echo.

set GOOS=
set GOARCH=
set CGO_ENABLED=

pause
endlocal
