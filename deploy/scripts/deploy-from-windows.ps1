# Build Linux API binary + deploy bundle on Windows (requires Go)
# Upload bundle to server manually with scp / WinSCP

$ErrorActionPreference = "Stop"
$Root = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$Bundle = Join-Path $Root "deploy\bundle"

Write-Host "Preparing bundle folders..."
if ((Test-Path $Bundle) -and ((Resolve-Path $Bundle).Path -like (Join-Path $Root "deploy\bundle"))) {
  Remove-Item -LiteralPath $Bundle -Recurse -Force
}
New-Item -ItemType Directory -Force -Path (Join-Path $Bundle "bin") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $Bundle "config") | Out-Null

Write-Host "Building Linux API binary..."
Push-Location (Join-Path $Root "backend")
$env:CGO_ENABLED = "0"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -ldflags="-s -w" -o (Join-Path $Bundle "bin\copilot-api") .\cmd\server
if ($LASTEXITCODE -ne 0) {
  throw "go build failed with exit code $LASTEXITCODE"
}
Pop-Location

Write-Host "Packaging bundle..."
Copy-Item -Recurse -Force (Join-Path $Root "backend\webrtc") (Join-Path $Bundle "bin\webrtc")
Copy-Item -Force (Join-Path $Root "deploy\scripts\install-api-ubuntu.sh") (Join-Path $Bundle "install.sh")
Copy-Item -Force (Join-Path $Root "deploy\env\production.api.env.example") (Join-Path $Bundle "config\env.example")
Copy-Item -Force (Join-Path $Root "deploy\nginx\api.copilotai.click.conf") (Join-Path $Bundle "config\")
Copy-Item -Force (Join-Path $Root "deploy\nginx\api.copilotai.click.initial.conf") (Join-Path $Bundle "config\")
Copy-Item -Force (Join-Path $Root "deploy\coturn\turnserver.conf") (Join-Path $Bundle "config\")
Copy-Item -Force (Join-Path $Root "deploy\systemd\copilot-api.service") (Join-Path $Bundle "config\")
Copy-Item -Force (Join-Path $Root "deploy\scripts\verify-webrtc-production.sh") (Join-Path $Bundle "verify-webrtc-production.sh")

Write-Host ""
Write-Host "Bundle ready: $Bundle"
Write-Host ""
Write-Host "Next (replace USER and SERVER_IP):"
Write-Host "  scp -r deploy/bundle/* USER@SERVER_IP:/tmp/copilot-bundle/"
Write-Host "  ssh USER@SERVER_IP"
Write-Host "  cd /tmp/copilot-bundle"
Write-Host "  sudo bash install.sh"
Write-Host "  sudo nano /etc/copilot-api/env"
Write-Host "  sudo nano /etc/turnserver.conf"
Write-Host "  sudo certbot --nginx -d api.copilotai.click"
Write-Host "  bash verify-webrtc-production.sh https://api.copilotai.click"
