$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $taskRoot
try {
    docker build -f deploy/go2rtc.Dockerfile --target build --build-arg TARGETOS=windows --build-arg TARGETARCH=amd64 -t camerahub/gateway-build:windows-control .
    if ($LASTEXITCODE -ne 0) { throw 'Windows gateway build failed.' }
    $taskRuntime = Join-Path $taskRoot 'runtime/windows'
    New-Item -ItemType Directory -Force -Path $taskRuntime | Out-Null
    $taskContainer = docker create camerahub/gateway-build:windows-control
    if ($LASTEXITCODE -ne 0) { throw 'Cannot extract Windows gateway.' }
    try {
        $taskBinary = Join-Path $taskRuntime 'go2rtc.exe'
        docker cp "${taskContainer}:/go2rtc" $taskBinary
        if ($LASTEXITCODE -ne 0) { throw 'Cannot copy Windows gateway.' }
    } finally { docker rm $taskContainer | Out-Null }
    [IO.File]::WriteAllText(($taskBinary + '.sha256'), (Get-FileHash -LiteralPath $taskBinary -Algorithm SHA256).Hash.ToLowerInvariant())
    Write-Host 'Windows gateway built from the same pinned source and control patch as the Docker gateway.'
} finally { Pop-Location }
