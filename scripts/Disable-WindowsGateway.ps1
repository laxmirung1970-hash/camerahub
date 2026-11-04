# Return to the original Docker gateway while retaining the latest accounts/streams.
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Windows-Gateway.ps1')
Push-Location -LiteralPath $taskGatewayRoot
try {
    if (-not (Test-Path -LiteralPath $taskGatewaySettings)) { Write-Host 'Native gateway is not enabled.'; return }
    & (Join-Path $PSScriptRoot 'Stop-Live.ps1')
    if ($LASTEXITCODE -ne 0) { throw 'Could not stop live services safely.' }
    docker run --rm --mount 'type=volume,source=camerahub-windows-live_live-gateway,target=/state' --mount "type=bind,source=$taskGatewayState,target=/input,readonly" alpine:3.22 sh -c 'cp /state/go2rtc.yaml /state/go2rtc.pre-native.yaml && cp /input/go2rtc.yaml /state/go2rtc.yaml && chown 10001:10001 /state/go2rtc.yaml && chmod 600 /state/go2rtc.yaml'
    if ($LASTEXITCODE -ne 0) { throw 'Could not copy gateway state back. Native files were preserved; run Start-Live.ps1 to resume.' }
    Move-Item -LiteralPath $taskGatewaySettings -Destination (Join-Path $taskGatewayState ('native.disabled-' + [Guid]::NewGuid().ToString('N') + '.json'))
    & (Join-Path $PSScriptRoot 'Start-Live.ps1') -NoBrowser
    Write-Host 'Original Docker gateway restored with current accounts and streams.'
} finally { Pop-Location }
