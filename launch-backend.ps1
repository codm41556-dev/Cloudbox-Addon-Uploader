param(
    [Parameter(Mandatory)] [string]$WorkDir,
    [Parameter(Mandatory)] [string]$DbPassword,
    [Parameter(Mandatory)] [string]$S3Endpoint,
    [string]$ApiKey = "",
    [Parameter(Mandatory)] [string]$BindAddr
)

Set-Location $WorkDir

$env:AWS_ACCESS_KEY_ID = "minioadmin"
$env:AWS_SECRET_ACCESS_KEY = "minioadmin123"
$env:AWS_REGION = "us-east-1"

Write-Host "Starting cloudbox-backend.exe ..." -ForegroundColor Cyan
Write-Host ""

& ".\cloudbox-backend.exe" `
    "-dbuser=cloudbox" `
    "-dbpass=$DbPassword" `
    "-dbaddr=127.0.0.1:3306" `
    "-dbproto=tcp" `
    "-dbname=cloudbox" `
    "-s3endpoint=$S3Endpoint" `
    "-apikey=$ApiKey" `
    "-addr=$BindAddr" `
    "-proto=tcp"

$exitCode = $LASTEXITCODE
Write-Host ""
if ($exitCode -eq 0) {
    Write-Host "cloudbox-backend.exe exited normally (code 0)." -ForegroundColor Green
} else {
    Write-Host "cloudbox-backend.exe EXITED / CRASHED (code $exitCode). Read whatever it printed above - that's the actual error." -ForegroundColor Red
}
Read-Host "Press Enter to close this window"
