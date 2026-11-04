$ErrorActionPreference = 'Stop'
Push-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
try {
    . (Join-Path $PSScriptRoot 'Windows-Gateway.ps1')
    . (Join-Path $PSScriptRoot 'Windows-Relay.ps1')
    if (Get-WindowsRelayEnabled) {
        $env:CAMERAHUB_NATIVE_GATEWAY_URL=Get-NativeGatewayUrl
        try { docker compose -f compose.windows-live.yaml -f compose.windows-relay.yaml stop } finally {Remove-Item Env:CAMERAHUB_NATIVE_GATEWAY_URL -ErrorAction SilentlyContinue}
        if ($LASTEXITCODE -ne 0) {throw 'Live stop failed.'}
        Stop-WindowsRelay
    } else {
        docker compose -f compose.windows-live.yaml stop
        if ($LASTEXITCODE -ne 0) { throw 'Live stop failed.' }
        if (Test-Path -LiteralPath $taskGatewaySettings) { Stop-NativeGateway }
    }
} finally { Pop-Location }
