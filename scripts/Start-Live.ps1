param([switch]$NoBrowser)
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $taskRoot
try {
    . (Join-Path $PSScriptRoot 'Windows-Gateway.ps1')
    . (Join-Path $PSScriptRoot 'Windows-Relay.ps1')
    docker info --format '{{.ServerVersion}}' | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Open Docker Desktop first.' }
    $taskAppImage = docker image ls -q camerahub/app:1.0.0-amd64
    $taskGatewayTag = 'camerahub/gateway:1.9.14-control-amd64'
    if (Get-WindowsRelayEnabled) { $taskGatewayTag = 'camerahub/gateway:1.9.14-relay-amd64' }
    $taskGatewayImage = docker image ls -q $taskGatewayTag
    $taskPreparedImages = Join-Path $taskRoot 'camerahub-windows-images.tar'
    if ((-not $taskAppImage -or -not $taskGatewayImage) -and (Test-Path -LiteralPath $taskPreparedImages)) {
        docker load -i $taskPreparedImages
        if ($LASTEXITCODE -ne 0) { throw 'Prepared image import failed.' }
        $taskAppImage = docker image ls -q camerahub/app:1.0.0-amd64
        $taskGatewayImage = docker image ls -q $taskGatewayTag
    }
    if (-not $taskAppImage -or -not $taskGatewayImage) {
        if (-not (Test-Path -LiteralPath (Join-Path $taskRoot 'Dockerfile'))) { throw 'Load the prepared Windows images first using Load-Images.ps1.' }
        docker build -t camerahub/app:1.0.0-amd64 .
        if ($LASTEXITCODE -ne 0) { throw 'App build failed.' }
        docker build -f deploy/go2rtc.Dockerfile -t $taskGatewayTag .
        if ($LASTEXITCODE -ne 0) { throw 'Gateway build failed.' }
    }
    if (Get-WindowsRelayEnabled) {
        Start-WindowsRelay
        $env:CAMERAHUB_NATIVE_GATEWAY_URL = Get-NativeGatewayUrl
        try {
            docker compose -f compose.windows-live.yaml -f compose.windows-relay.yaml up -d --no-build
        } finally { Remove-Item Env:CAMERAHUB_NATIVE_GATEWAY_URL -ErrorAction SilentlyContinue }
    } elseif (Test-Path -LiteralPath $taskGatewaySettings) {
        Start-NativeGateway
        $env:CAMERAHUB_NATIVE_GATEWAY_URL = Get-NativeGatewayUrl
        try {
            docker compose -f compose.windows-live.yaml -f compose.windows-native.yaml up -d --no-build app
        } finally { Remove-Item Env:CAMERAHUB_NATIVE_GATEWAY_URL -ErrorAction SilentlyContinue }
    } else {
        docker compose -f compose.windows-live.yaml up -d --no-build
    }
    if ($LASTEXITCODE -ne 0) { throw 'Live startup failed.' }
    $taskReady = $false
    for ($taskTry = 0; $taskTry -lt 30; $taskTry++) {
        try {
            $taskBootstrap = Invoke-RestMethod 'http://127.0.0.1:17801/api/bootstrap'
            if ($taskBootstrap.mode -ne 'live') { throw 'Unexpected app mode.' }
            $taskReady = $true
            break
        } catch { Start-Sleep -Seconds 1 }
    }
    if (-not $taskReady) { throw 'Live app did not become ready on port 17801. Check Docker logs and port conflicts.' }
    Write-Host 'CameraHub LIVE (this Windows PC only): http://127.0.0.1:17801'
    Write-Host 'Add cameras by IP. Demo data is separate. This PC and Docker must stay running while viewing.'
    if ($taskBootstrap.needsSetup) {
        Write-Host 'First setup: create your CameraHub owner account using the setup code.'
        if ($NoBrowser) {
            Write-Host 'Show code: powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/Show-SetupCode.ps1 -Mode windows-live'
        } else {
            & (Join-Path $PSScriptRoot 'Show-SetupCode.ps1') -Mode windows-live
        }
    }
    if (-not $NoBrowser) { Start-Process 'http://127.0.0.1:17801' }
} finally { Pop-Location }
