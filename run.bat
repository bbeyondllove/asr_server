@echo off
echo Building the project...
go build -o asr_server.exe main.go
if %errorlevel% neq 0 (
    echo Build failed!
    exit /b %errorlevel%
)
echo Starting the server...
.\asr_server.exe
