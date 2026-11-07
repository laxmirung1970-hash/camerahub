$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
$taskRelease = Join-Path $taskRoot 'releases'
New-Item -ItemType Directory -Force -Path $taskRelease | Out-Null
Push-Location -LiteralPath $taskRoot
try {
    foreach ($taskRequired in @('camerahub-tv-1.3.3.apk','camerahub-nas-arm64-images.tar','compose.nas.yaml')) {
        if (-not (Test-Path -LiteralPath (Join-Path $taskRelease $taskRequired))) { throw "Missing release file: $taskRequired" }
    }
    if (-not (Test-Path -LiteralPath (Join-Path $taskRoot 'runtime/windows/go2rtc.exe.sha256'))) { throw 'Build the Windows native gateway before packaging.' }
    $taskStage = Join-Path $taskRelease ('stage-' + [Guid]::NewGuid().ToString('N'))
    $taskWindows = Join-Path $taskStage 'CameraHub-Windows-1.0.0'
    $taskSource = Join-Path $taskStage 'CameraHub-Source-1.0.0'
    New-Item -ItemType Directory -Force -Path $taskWindows,$taskSource | Out-Null
    docker save -o (Join-Path $taskWindows 'camerahub-windows-images.tar') camerahub/app:1.0.0-amd64 camerahub/gateway:1.9.14-control-amd64 camerahub/gateway:1.9.14-relay-amd64
    if ($LASTEXITCODE -ne 0) { throw 'Windows image export failed.' }
    foreach ($taskName in @('README.md','compose.yaml','compose.windows-live.yaml','compose.windows-native.yaml','compose.windows-relay.yaml','Start-CameraHub.cmd','Start-CameraHub-Live.cmd')) { Copy-Item -LiteralPath (Join-Path $taskRoot $taskName) -Destination $taskWindows }
    New-Item -ItemType Directory -Force -Path (Join-Path $taskWindows 'runtime/windows') | Out-Null
    foreach ($taskName in @('go2rtc.exe','go2rtc.exe.sha256')) { Copy-Item -LiteralPath (Join-Path $taskRoot ('runtime/windows/' + $taskName)) -Destination (Join-Path $taskWindows 'runtime/windows') }
    foreach ($taskName in @('docs','media','vendor')) { Copy-Item -LiteralPath (Join-Path $taskRoot $taskName) -Destination $taskWindows -Recurse }
    New-Item -ItemType Directory -Force -Path (Join-Path $taskWindows 'scripts') | Out-Null
    foreach ($taskName in @('Start-Demo.ps1','Stop-Demo.ps1','Start-Live.ps1','Stop-Live.ps1','Show-SetupCode.ps1','Check-System.ps1','Windows-Gateway.ps1','Windows-Relay.ps1','windows-udp-relay.mjs','Enable-WindowsGateway.ps1','Disable-WindowsGateway.ps1','Watch-Live.ps1','Install-LiveWatchdog.ps1')) { Copy-Item -LiteralPath (Join-Path $PSScriptRoot $taskName) -Destination (Join-Path $taskWindows 'scripts') }
    Set-Content -LiteralPath (Join-Path $taskWindows 'Load-Images.ps1') -Encoding UTF8 -Value @'
$ErrorActionPreference = 'Stop'
docker load -i (Join-Path $PSScriptRoot 'camerahub-windows-images.tar')
if ($LASTEXITCODE -ne 0) { throw 'Image import failed.' }
Write-Host 'Ready. Open Start-CameraHub.cmd.'
'@
    # Explicit allowlist: never copy data, keys, .env, caches or build output.
    foreach ($taskName in @('README.md','compose.yaml','compose.windows-live.yaml','compose.windows-native.yaml','compose.windows-relay.yaml','Dockerfile','.dockerignore','.gitignore','Start-CameraHub.cmd','Start-CameraHub-Live.cmd')) { Copy-Item -LiteralPath (Join-Path $taskRoot $taskName) -Destination $taskSource }
    foreach ($taskName in @('backend','docs','media','vendor','scripts','deploy')) { Copy-Item -LiteralPath (Join-Path $taskRoot $taskName) -Destination $taskSource -Recurse }
    $taskWeb = Join-Path $taskSource 'web'
    New-Item -ItemType Directory -Force -Path $taskWeb | Out-Null
    foreach ($taskName in @('src','public','package.json','package-lock.json','tsconfig.json','vite.config.ts','index.html')) { Copy-Item -LiteralPath (Join-Path $taskRoot ('web/' + $taskName)) -Destination $taskWeb -Recurse }
    $taskAndroid = Join-Path $taskSource 'android-tv'
    New-Item -ItemType Directory -Force -Path (Join-Path $taskAndroid 'app') | Out-Null
    foreach ($taskName in @('build.gradle.kts','settings.gradle.kts','gradle.properties','build.sh')) { Copy-Item -LiteralPath (Join-Path $taskRoot ('android-tv/' + $taskName)) -Destination $taskAndroid }
    Copy-Item -LiteralPath (Join-Path $taskRoot 'android-tv/app/src') -Destination (Join-Path $taskAndroid 'app') -Recurse
    Copy-Item -LiteralPath (Join-Path $taskRoot 'android-tv/app/build.gradle.kts') -Destination (Join-Path $taskAndroid 'app')
    Compress-Archive -LiteralPath $taskWindows -DestinationPath (Join-Path $taskRelease 'CameraHub-Windows-1.0.0.zip') -Force
    Compress-Archive -LiteralPath $taskSource -DestinationPath (Join-Path $taskRelease 'CameraHub-Source-1.0.0.zip') -Force
    $taskHashes = Get-ChildItem -LiteralPath $taskRelease -File | Where-Object { $_.Extension -in @('.zip','.apk','.tar','.yaml') } | ForEach-Object {
        $taskHash = Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256
        '{0}  {1}' -f $taskHash.Hash.ToLowerInvariant(), $_.Name
    }
    [IO.File]::WriteAllLines((Join-Path $taskRelease 'SHA256SUMS.txt'), $taskHashes, [Text.UTF8Encoding]::new($false))
    # Remove only the exact staging directory created above, after checking its absolute boundary.
    $taskResolvedStage = [IO.Path]::GetFullPath($taskStage)
    $taskResolvedRelease = [IO.Path]::GetFullPath($taskRelease).TrimEnd('\') + '\'
    if (-not $taskResolvedStage.StartsWith($taskResolvedRelease,[StringComparison]::OrdinalIgnoreCase)) { throw 'Unsafe staging path.' }
    Remove-Item -LiteralPath $taskResolvedStage -Recurse -Force
    Write-Host 'Release archives and SHA256SUMS.txt are ready in releases.'
} finally { Pop-Location }
