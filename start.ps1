$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$config = Join-Path $root "config.bookshelves.json"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
  throw "Go executable was not found in PATH"
}

if (-not (Test-Path $config)) {
  throw "Missing config: $config"
}

Get-Process server,mynewmangaui -ErrorAction SilentlyContinue | Stop-Process -Force

Set-Location $root
Write-Host "Starting: go run ./cmd/server"
Write-Host "Config:" $config
& go run ./cmd/server -config $config
