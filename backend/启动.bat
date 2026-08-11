@echo off
chcp 65001 >nul
cd /d "%~dp0"

REM ============================================================
REM  WebMap 后端 - 双击启动
REM  直接进入交互式控制台菜单：配置向导 / 启动服务 / 查看状态
REM ============================================================

if not exist "webmap.exe" (
    echo.
    echo  [提示] 未找到 webmap.exe
    echo         请先在本目录执行:  go build -o webmap.exe .
    echo.
    pause
    exit /b 1
)

webmap.exe

echo.
pause
