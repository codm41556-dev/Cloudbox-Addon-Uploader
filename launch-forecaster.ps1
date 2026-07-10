param(
    [Parameter(Mandatory)] [string]$WorkDir,
    [Parameter(Mandatory)] [string]$ApiUrl,
    [Parameter(Mandatory)] [string]$BindAddr
)

Set-Location $WorkDir

$env:API_URL = $ApiUrl

Write-Host "Starting forecaster.exe ..." -ForegroundColor Cyan
Write-Host ""

& ".\forecaster.exe" `
    "-addr=$BindAddr" `
    "-proto=tcp"

$exitCode = $LASTEXITCODE
Write-Host ""
if ($exitCode -eq 0) {
    Write-Host "forecaster.exe exited normally (code 0)." -ForegroundColor Green
} else {
    Write-Host "forecaster.exe EXITED / CRASHED (code $exitCode). Read whatever it printed above - that's the actual error." -ForegroundColor Red
}
Read-Host "Press Enter to close this window"
