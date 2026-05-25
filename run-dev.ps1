# Run backend API + Flutter web dashboard (one command)
# Usage: .\run-dev.ps1
# Stop: close both terminal windows, or Ctrl+C in the Flutter window then close the API window

$ErrorActionPreference = "Stop"
$Root = $PSScriptRoot
$Backend = Join-Path $Root "backend"
$Frontend = Join-Path $Root "copilot_app_frontend"
$ApiHost = if ($env:API_HOST) { $env:API_HOST } else { "127.0.0.1" }
$WebHost = if ($env:WEB_HOST) { $env:WEB_HOST } else { "127.0.0.1" }

function Test-PortAvailable {
  param(
    [Parameter(Mandatory = $true)][int]$Port,
    [string]$HostName = "127.0.0.1"
  )

  $listener = $null
  try {
    $address = $null
    if (-not [System.Net.IPAddress]::TryParse($HostName, [ref]$address)) {
      $address = [System.Net.Dns]::GetHostAddresses($HostName) |
        Where-Object { $_.AddressFamily -eq [System.Net.Sockets.AddressFamily]::InterNetwork } |
        Select-Object -First 1
    }
    if ($address -eq $null) {
      return $false
    }
    $listener = [System.Net.Sockets.TcpListener]::new($address, $Port)
    $listener.Start()
    return $true
  } catch {
    return $false
  } finally {
    if ($listener -ne $null) {
      $listener.Stop()
    }
  }
}

function Get-AvailablePort {
  param(
    [Parameter(Mandatory = $true)][int[]]$Candidates,
    [Parameter(Mandatory = $true)][string]$Purpose,
    [string]$HostName = "127.0.0.1"
  )

  foreach ($candidate in $Candidates) {
    if (Test-PortAvailable -Port $candidate -HostName $HostName) {
      if ($candidate -ne $Candidates[0]) {
        Write-Host "$Purpose port $($Candidates[0]) is not available; using $candidate instead." -ForegroundColor Yellow
      }
      return $candidate
    }
  }

  throw "No available $Purpose port found in: $($Candidates -join ', ')"
}

if ($env:PORT) {
  $ApiPort = [int]$env:PORT
  if (-not (Test-PortAvailable -Port $ApiPort -HostName $ApiHost)) {
    Write-Error "API port $ApiPort is already in use on $ApiHost. Set PORT to a free port or stop the process using it."
  }
} else {
  $ApiPort = Get-AvailablePort -Purpose "API" -Candidates (@(8081) + (18081..18090)) -HostName $ApiHost
}

if ($env:WEB_PORT) {
  $WebPort = [int]$env:WEB_PORT
  if (-not (Test-PortAvailable -Port $WebPort -HostName $WebHost)) {
    Write-Error "Flutter web port $WebPort is not available on $WebHost. Set WEB_PORT to a free port."
  }
} else {
  $WebPort = Get-AvailablePort -Purpose "Flutter web" -Candidates (8082..8099) -HostName $WebHost
}

$ApiUrl = if ($env:API_URL) { $env:API_URL } else { "http://${ApiHost}:$ApiPort/api" }

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
  Write-Error "Go is not installed or not on PATH. Install from https://go.dev/dl/"
}
if (-not (Get-Command flutter -ErrorAction SilentlyContinue)) {
  Write-Error "Flutter is not installed or not on PATH."
}

Write-Host "Starting backend API in a new window..." -ForegroundColor Cyan
$BackendForCommand = $Backend.Replace("'", "''")
$ApiHostForCommand = $ApiHost.Replace("'", "''")
Start-Process powershell -ArgumentList @(
  "-NoExit",
  "-Command",
  "`$env:PORT = '$ApiPort'; `$env:API_HOST = '$ApiHostForCommand'; Set-Location -LiteralPath '$BackendForCommand'; Write-Host 'Backend API - close this window to stop the API' -ForegroundColor Green; go run ./cmd/server"
)

Write-Host "Waiting for API to start..." -ForegroundColor Gray
Start-Sleep -Seconds 4

Write-Host "Starting Flutter dashboard (this window)..." -ForegroundColor Cyan
Write-Host "  Dashboard: http://${WebHost}:$WebPort" -ForegroundColor Yellow
Write-Host "  API:       $ApiUrl" -ForegroundColor Yellow
Write-Host "  Login:     any stored SQLite user with that user's password" -ForegroundColor Yellow
Write-Host "  Fresh seed: admin@sapience.com and fleet@sapience.com use password123 when the database is empty" -ForegroundColor DarkYellow

Set-Location $Frontend
flutter pub get | Out-Null
flutter run -d chrome --web-hostname=$WebHost --web-port=$WebPort `
  --dart-define=API_URL=$ApiUrl `
  --dart-define=ENV=development `
  --dart-define=ENABLE_LOGGING=true
