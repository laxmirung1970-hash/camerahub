param([switch]$NoBrowser, [switch]$Rebuild)
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $taskRoot
try {
    docker info --format '{{.ServerVersion}}' | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Open Docker Desktop first.' }
    if (-not (Test-Path -LiteralPath (Join-Path $taskRoot 'media/scene-1-low.mp4'))) {
        node scripts/generate-media.mjs
        if ($LASTEXITCODE -ne 0) { throw 'Cannot generate demo video.' }
    }
    $taskAppImage = docker image ls -q camerahub/app:1.0.0-amd64
    $taskGatewayImage = docker image ls -q camerahub/gateway:1.9.14-control-amd64
    $taskPreparedImages = Join-Path $taskRoot 'camerahub-windows-images.tar'
    if ((-not $taskAppImage -or -not $taskGatewayImage) -and (Test-Path -LiteralPath $taskPreparedImages)) {
        docker load -i $taskPreparedImages
        if ($LASTEXITCODE -ne 0) { throw 'Prepared image import failed.' }
        $taskAppImage = docker image ls -q camerahub/app:1.0.0-amd64
        $taskGatewayImage = docker image ls -q camerahub/gateway:1.9.14-control-amd64
    }
    if ($Rebuild -or -not $taskAppImage -or -not $taskGatewayImage) {
        docker build -t camerahub/app:1.0.0-amd64 .
        if ($LASTEXITCODE -ne 0) { throw 'App build failed.' }
        docker build -f deploy/go2rtc.Dockerfile -t camerahub/gateway:1.9.14-control-amd64 .
        if ($LASTEXITCODE -ne 0) { throw 'Build failed.' }
    }
    docker compose up -d --no-build
    if ($LASTEXITCODE -ne 0) { throw 'Startup failed.' }
    $taskReady = $false
    for ($taskTry = 0; $taskTry -lt 30; $taskTry++) {
        try { Invoke-RestMethod 'http://127.0.0.1:17800/api/health' | Out-Null; $taskReady = $true; break } catch { Start-Sleep -Seconds 1 }
    }
    if (-not $taskReady) { throw 'App did not become ready. Run scripts\Check-System.ps1.' }
    Write-Host 'CameraHub demo: http://127.0.0.1:17800'
    Write-Host 'For first setup: run scripts\Show-SetupCode.ps1'
    if (-not $NoBrowser) { Start-Process 'http://127.0.0.1:17800' }
} finally { Pop-Location }
