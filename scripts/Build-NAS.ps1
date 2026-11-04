$ErrorActionPreference = 'Stop'
Push-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
try {
    docker buildx build --platform linux/arm64 --load -t camerahub/app:1.0.0-arm64 .
    if ($LASTEXITCODE -ne 0) { throw 'ARM64 app build failed.' }
    docker buildx build --platform linux/arm64 --load -f deploy/go2rtc.Dockerfile -t camerahub/gateway:1.9.14-control-arm64 .
    if ($LASTEXITCODE -ne 0) { throw 'ARM64 gateway build failed.' }
    New-Item -ItemType Directory -Force releases | Out-Null
    docker save -o releases/camerahub-nas-arm64-images.tar camerahub/app:1.0.0-arm64 camerahub/gateway:1.9.14-control-arm64
    if ($LASTEXITCODE -ne 0) { throw 'Cannot export NAS images.' }
    Copy-Item -LiteralPath deploy/compose.nas.yaml -Destination releases/compose.nas.yaml
} finally { Pop-Location }
