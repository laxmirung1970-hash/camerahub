$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $taskRoot
try {
    node scripts/prepare-signing.mjs
    docker build -f deploy/android-builder.Dockerfile -t camerahub/android-builder:1 .
    if ($LASTEXITCODE -ne 0) { throw 'Android toolchain build failed.' }
    New-Item -ItemType Directory -Force -Path (Join-Path $taskRoot 'releases') | Out-Null
    docker run --rm --env-file (Join-Path $taskRoot 'data/signing/signing.env') --mount "type=bind,source=$taskRoot\android-tv,target=/src" --mount "type=bind,source=$taskRoot\data\signing,target=/keys" --mount "type=bind,source=$taskRoot\releases,target=/out" --mount 'type=volume,source=camerahub-gradle,target=/gradle-cache' camerahub/android-builder:1 sh /src/build.sh
    if ($LASTEXITCODE -ne 0) { throw 'APK build or verification failed.' }
    Write-Host 'APK: releases\camerahub-tv-1.2.0.apk'
} finally { Pop-Location }
