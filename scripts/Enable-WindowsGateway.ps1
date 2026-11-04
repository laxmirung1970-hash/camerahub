$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Windows-Gateway.ps1')
Push-Location -LiteralPath $taskGatewayRoot
try {
    if (-not (Test-Path -LiteralPath $taskGatewayExe)) { & (Join-Path $PSScriptRoot 'Build-WindowsGateway.ps1') }
    if (-not (Test-Path -LiteralPath $taskGatewaySettings)) {
        $taskVolume = @(docker volume ls --format '{{.Name}}' --filter name=^camerahub-windows-live_live-gateway$)
        if ($taskVolume -notcontains 'camerahub-windows-live_live-gateway') { throw 'Start Windows live in Docker and finish initial setup before migration.' }
        New-Item -ItemType Directory -Force -Path $taskGatewayState | Out-Null
        $taskIdentity = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
        icacls.exe $taskGatewayState /inheritance:r /grant:r "${taskIdentity}:(OI)(CI)F" 'SYSTEM:(OI)(CI)F' | Out-Null
        if ($LASTEXITCODE -ne 0) { throw 'Cannot protect native gateway state with Windows permissions.' }
        # Stop writers before copying. The original Docker volume is preserved for rollback.
        docker compose -f compose.windows-live.yaml stop app gateway
        if ($LASTEXITCODE -ne 0) { throw 'Could not stop Docker services safely.' }
        docker run --rm --mount 'type=volume,source=camerahub-windows-live_live-gateway,target=/state,readonly' --mount "type=bind,source=$taskGatewayState,target=/out" alpine:3.22 cp /state/go2rtc.yaml /out/go2rtc.yaml
        if ($LASTEXITCODE -ne 0) {
            docker compose -f compose.windows-live.yaml up -d --no-build | Out-Null
            throw 'Gateway migration failed; original Docker services restored.'
        }
        $taskRandom = New-Object byte[] 32
        $taskRng = [Security.Cryptography.RandomNumberGenerator]::Create()
        try { $taskRng.GetBytes($taskRandom) } finally { $taskRng.Dispose() }
        $taskSecret = -join ($taskRandom | ForEach-Object { $_.ToString('x2') })
        $taskOverlay = @{
            api = @{ listen='127.0.0.1:17984'; username='camerahub'; password=$taskSecret; local_auth=$true; allow_paths=@('/api','/api/streams','/api/ws','/api/webrtc','/api/stream.m3u8','/api/hls/playlist.m3u8','/api/hls/init.mp4','/api/hls/segment.m4s','/api/hls/segment.ts','/api/xiaomi','/api/xiaomi/command') }
            rtsp = @{ listen='127.0.0.1:18554'; username='camerahub'; password=$taskSecret }
            webrtc = @{ listen='' }
            log = @{ level='error' }
            app = @{ modules=@('api','ws','http','rtsp','mp4','hls','mjpeg','exec','ffmpeg','xiaomi') }
        }
        [IO.File]::WriteAllText($taskGatewaySettings, ($taskOverlay | ConvertTo-Json -Depth 5), [Text.UTF8Encoding]::new($false))
    }
    & (Join-Path $PSScriptRoot 'Start-Live.ps1') -NoBrowser
    Write-Host 'Windows native gateway enabled. CameraHub address and camera accounts are unchanged.'
} finally { Pop-Location }
