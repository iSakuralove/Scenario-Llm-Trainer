[CmdletBinding()]
param(
  [switch]$StartApi,
  [switch]$Detached
)

$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot

try {
  Write-Host 'This will remove the PostgreSQL Docker volume and recreate seed data on next API startup.' -ForegroundColor Yellow
  docker compose down -v
  if ($LASTEXITCODE -ne 0) {
    throw 'docker compose down -v failed'
  }

  if ($StartApi) {
    $composeArgs = @('compose', 'up', '--build')
    if ($Detached) {
      $composeArgs += '-d'
    }
    $composeArgs += 'api'
    docker @composeArgs
    if ($LASTEXITCODE -ne 0) {
      throw 'docker compose up --build api failed'
    }
  } else {
    Write-Host 'Data volume removed. Run `docker compose up --build api` to recreate demo accounts and seed questions.'
  }
} finally {
  Pop-Location
}
