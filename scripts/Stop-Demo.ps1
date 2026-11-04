$ErrorActionPreference = 'Stop'
Push-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
try { docker compose stop; if ($LASTEXITCODE -ne 0) { throw 'Stop failed.' } } finally { Pop-Location }
