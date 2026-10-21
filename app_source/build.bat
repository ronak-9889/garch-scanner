@echo off
rem Builds the Windows app into ..\software. Needs Go 1.22+ (https://go.dev/dl). No other dependencies.
cd /d "%~dp0"
if not exist ..\software mkdir ..\software
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags "-s -w" -o ..\software\garch-volatility-windows-amd64.exe .
if errorlevel 1 (echo BUILD FAILED) else (echo Built ..\software\garch-volatility-windows-amd64.exe)
pause
