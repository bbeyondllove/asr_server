$ErrorActionPreference = "Stop"
Write-Host "Building the project..." -ForegroundColor Cyan
go build -o asr_server.exe main.go
if ($LASTEXITCODE -ne 0) {
    Write-Host "Build failed!" -ForegroundColor Red
    exit $LASTEXITCODE
}
Write-Host "Starting the server..." -ForegroundColor Cyan
.\asr_server.exe
