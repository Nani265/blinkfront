param(
  [Parameter(Mandatory = $true)]
  [string]$SshTarget,

  [string]$ApiDomain = "api.copilotai.click",
  [string]$TurnDomain = "turn.copilotai.click",
  [string]$TurnUsername = "copilot_turn_user",
  [string]$TurnSecret = "",
  [string]$PublicIp = "",
  [string]$CertbotEmail = "",
  [string]$IdentityFile = "",
  [switch]$SkipCertbot,
  [switch]$BuildFlutter
)

$ErrorActionPreference = "Stop"
$Root = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$Bundle = Join-Path $Root "deploy\bundle"

function New-TurnSecret {
  $bytes = New-Object byte[] 32
  [System.Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
  return [Convert]::ToBase64String($bytes).TrimEnd("=").Replace("+", "_").Replace("/", "-")
}

function Quote-Bash {
  param([string]$Value)
  return "'" + ($Value -replace "'", "'""'""'") + "'"
}

if ([string]::IsNullOrWhiteSpace($TurnSecret)) {
  $TurnSecret = New-TurnSecret
  Write-Host "Generated TURN secret for this deploy." -ForegroundColor Yellow
}

$sshArgs = @()
if (-not [string]::IsNullOrWhiteSpace($IdentityFile)) {
  $sshArgs += @("-i", $IdentityFile)
}

Write-Host "==> Building deploy bundle" -ForegroundColor Cyan
& (Join-Path $Root "deploy\scripts\deploy-from-windows.ps1")

Write-Host "==> Preparing remote upload folder" -ForegroundColor Cyan
& ssh @sshArgs $SshTarget "rm -rf /tmp/copilot-bundle && mkdir -p /tmp"

Write-Host "==> Uploading bundle to $SshTarget" -ForegroundColor Cyan
& scp @sshArgs -r $Bundle "${SshTarget}:/tmp/copilot-bundle"

$runCertbot = if ($SkipCertbot) { "0" } else { "1" }
$remoteInstall = @(
  "API_DOMAIN=$(Quote-Bash $ApiDomain)",
  "TURN_DOMAIN=$(Quote-Bash $TurnDomain)",
  "TURN_USERNAME=$(Quote-Bash $TurnUsername)",
  "TURN_SECRET=$(Quote-Bash $TurnSecret)",
  "TURN_URL=$(Quote-Bash "turn:${TurnDomain}:3478")",
  "PUBLIC_IP=$(Quote-Bash $PublicIp)",
  "RUN_CERTBOT=$(Quote-Bash $runCertbot)",
  "CERTBOT_EMAIL=$(Quote-Bash $CertbotEmail)",
  "bash /tmp/copilot-bundle/install.sh"
) -join " "

Write-Host "==> Installing API, nginx, coturn, and SSL on server" -ForegroundColor Cyan
& ssh @sshArgs $SshTarget "sudo bash -lc $(Quote-Bash $remoteInstall)"

Write-Host "==> Verifying deployed WebRTC routes" -ForegroundColor Cyan
& ssh @sshArgs $SshTarget "bash /tmp/copilot-bundle/verify-webrtc-production.sh https://$ApiDomain"

if ($BuildFlutter) {
  Write-Host "==> Building Flutter web with production WebRTC config" -ForegroundColor Cyan
  Push-Location (Join-Path $Root "copilot_app_frontend")
  flutter build web --release `
    --dart-define=API_URL="https://$ApiDomain/api" `
    --dart-define=ENV=production `
    --dart-define=SIGNALING_WS_URL="wss://$ApiDomain/api/webrtc/ws" `
    --dart-define=STUN_URL="stun:stun.l.google.com:19302" `
    --dart-define=TURN_URL="turn:${TurnDomain}:3478" `
    --dart-define=TURN_USERNAME="$TurnUsername" `
    --dart-define=TURN_PASSWORD="$TurnSecret"
  Pop-Location
  Write-Host "Flutter build ready: $(Join-Path $Root 'copilot_app_frontend\build\web')" -ForegroundColor Green
}

Write-Host ""
Write-Host "Done." -ForegroundColor Green
Write-Host "API: https://$ApiDomain/api"
Write-Host "Publisher: https://$ApiDomain/api/webrtc/publisher/?vehicleId=1&token=TOKEN"
Write-Host "TURN URL: turn:${TurnDomain}:3478"
Write-Host "TURN username: $TurnUsername"
Write-Host "TURN secret used for this deploy: $TurnSecret"
