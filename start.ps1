$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$exe = Join-Path $root "bin\\mynewmangaui.exe"
$config = Join-Path $root "config.bookshelves.json"

if (-not (Test-Path $exe)) {
  throw "Missing executable: $exe"
}

Get-Process mynewmangaui -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Process -FilePath $exe -ArgumentList "-config", $config -WorkingDirectory $root

Write-Host "Started:" $exe
Write-Host "Config:" $config
