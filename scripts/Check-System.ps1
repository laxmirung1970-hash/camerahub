param([ValidateSet('demo','windows-live')][string]$Mode = 'demo')
$ErrorActionPreference = 'Stop'
Push-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
try {
    $taskConfigFile = 'compose.yaml'
    $taskPort = 17800
    if ($Mode -eq 'windows-live') { $taskConfigFile = 'compose.windows-live.yaml'; $taskPort = 17801 }
    docker compose -f $taskConfigFile ps
    Invoke-RestMethod "http://127.0.0.1:$taskPort/api/health" | Format-List
    if ($Mode -eq 'windows-live') {
        . (Join-Path $PSScriptRoot 'Windows-Gateway.ps1')
        . (Join-Path $PSScriptRoot 'Windows-Relay.ps1')
        if (Get-WindowsRelayEnabled) {
            Write-Host 'Video gateway: Docker + authenticated Windows UDP relay.'
            Write-Host ('Relay ready: ' + (Test-WindowsRelay))
        } elseif (Test-Path -LiteralPath $taskGatewaySettings) {
            Write-Host 'Video gateway: Windows native (authenticated loopback only).'
            $taskNativePort = [int](Get-NativeGatewaySettings).api.listen.Split(':')[-1]
            Get-NetTCPConnection -LocalPort $taskNativePort -State Listen -ErrorAction SilentlyContinue | Select-Object LocalAddress,LocalPort
        }
    }
    $taskContainers = @(docker compose -f $taskConfigFile ps -q)
    if ($taskContainers.Count -gt 0) {
        docker stats --no-stream --format 'table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}' @taskContainers
    }
} finally { Pop-Location }
