# One watchdog pass for Windows live (run every few minutes by Install-LiveWatchdog.ps1).
# Restarts only the Node UDP relay, and only while the live app container is running:
# Stop-Live.ps1 stops the containers, so a deliberate stop is left alone.
# Writes data/windows-live-gateway/watchdog.log only when it acts or fails. Never prints settings.
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
. (Join-Path $PSScriptRoot 'Windows-Gateway.ps1')
. (Join-Path $PSScriptRoot 'Windows-Relay.ps1')
$taskLog = Join-Path $taskGatewayState 'watchdog.log'
function Write-WatchdogLog($taskMessage) {
    if ((Test-Path -LiteralPath $taskLog) -and (Get-Item -LiteralPath $taskLog).Length -gt 256KB) {
        $taskKeep = Get-Content -LiteralPath $taskLog -Tail 200
        [IO.File]::WriteAllLines($taskLog, [string[]]$taskKeep)
    }
    [IO.File]::AppendAllText($taskLog, ((Get-Date).ToString('yyyy-MM-dd HH:mm:ss') + ' ' + $taskMessage + [Environment]::NewLine))
}
$taskMutex = [Threading.Mutex]::new($false, 'Local\CameraHubLiveWatchdog')
if (-not $taskMutex.WaitOne(0)) { exit 0 }
Push-Location -LiteralPath $taskRoot
try {
    if (-not (Get-WindowsRelayEnabled)) { exit 0 }
    $taskState = docker inspect -f '{{.State.Running}}' camerahub-windows-live-app-1 2>$null
    # Docker Desktop not up yet, or the owner stopped live: nothing to do.
    if ($LASTEXITCODE -ne 0 -or $taskState -ne 'true') { exit 0 }
    if (Test-WindowsRelay) { exit 0 }
    Start-Sleep -Seconds 3
    if (Test-WindowsRelay) { exit 0 }
    Write-WatchdogLog 'relay not answering; restarting it'
    # A hung relay keeps its PID, and Start-WindowsRelay would reuse it.
    Stop-WindowsRelay
    Start-WindowsRelay
    Write-WatchdogLog 'relay restarted'
} catch {
    Write-WatchdogLog ('watchdog failed: ' + $_.Exception.Message)
    exit 1
} finally {
    Pop-Location
    $taskMutex.ReleaseMutex()
}
