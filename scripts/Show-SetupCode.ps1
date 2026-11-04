param([ValidateSet('demo','windows-live')][string]$Mode = 'demo')
$ErrorActionPreference = 'Stop'
Push-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
try {
    if ($Mode -eq 'windows-live') {
        docker compose -f compose.windows-live.yaml exec -T app cat /data/live-setup-code.txt
    } else {
        docker compose exec -T app cat /data/demo-setup-code.txt
    }
    if ($LASTEXITCODE -ne 0) { Write-Host 'Already configured? Use the owner password instead.' }
} finally { Pop-Location }
