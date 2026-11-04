# Dot-source after Windows-Gateway.ps1. Never print service URLs or settings.
function Get-WindowsRelayEnabled {
    if (-not (Test-Path -LiteralPath $taskGatewaySettings)) { return $false }
    return (Get-NativeGatewaySettings).camerahub_runtime_mode -eq 'docker-relay'
}
function Get-WindowsRelayIdentity($taskProcessId) {
    if ($taskProcessId -le 0) { return $null }
    $taskProcess = Get-CimInstance Win32_Process -Filter "ProcessId=$taskProcessId" -ErrorAction SilentlyContinue
    $taskScript = Join-Path $taskGatewayRoot 'scripts/windows-udp-relay.mjs'
    if ($taskProcess -and ($taskProcess.Name -ne 'node.exe' -or -not $taskProcess.CommandLine.Contains($taskScript))) { throw 'Relay process identity does not match.' }
    return $taskProcess
}
function Test-WindowsRelay {
    $taskSettings=Get-NativeGatewaySettings
    # [string[]] flattens the array on both Windows PowerShell 5.1 (no pipeline unrolling) and PowerShell 7.
    $taskHosts=[string[]](Get-Content -LiteralPath (Join-Path $taskGatewayState 'relay-hosts.json') -Raw | ConvertFrom-Json)
    if ($taskHosts.Count -eq 0) { return $false }
    $taskClient=[Net.Sockets.TcpClient]::new()
    try {
        if (-not $taskClient.ConnectAsync('127.0.0.1',[int]$taskSettings.relay.port).Wait(1500)) { return $false }
        $taskStream=$taskClient.GetStream();$taskStream.ReadTimeout=1500
        $taskHello=@{host=$taskHosts[0];token=$taskSettings.api.password}|ConvertTo-Json -Compress
        $taskBytes=[Text.Encoding]::UTF8.GetBytes($taskHello+"`n")
        $taskStream.Write($taskBytes,0,$taskBytes.Length)
        return $taskStream.ReadByte() -eq 1
    } catch { return $false } finally {$taskClient.Dispose()}
}
function Prepare-WindowsRelay {
    $taskSettings=Get-NativeGatewaySettings
    if ([int]$taskSettings.relay.port -lt 17993 -or [int]$taskSettings.relay.port -gt 17999) { throw 'Invalid loopback relay port.' }
    $taskHostsFile=Join-Path $taskGatewayState 'relay-hosts.json'
    if (-not (Test-Path -LiteralPath $taskHostsFile)) { throw 'Camera relay allowlist is missing.' }
    $taskDocker=@{api=@{listen=':1984';username=$taskSettings.api.username;password=$taskSettings.api.password;local_auth=$true};rtsp=@{listen='127.0.0.1:8554'};webrtc=@{listen=''};app=@{modules=@('api','ws','http','rtsp','mp4','hls','mjpeg','exec','ffmpeg','xiaomi')};log=@{level='error'}}
    [IO.File]::WriteAllText((Join-Path $taskGatewayState 'docker-relay.json'),($taskDocker|ConvertTo-Json -Depth 12))
    [IO.File]::WriteAllText((Join-Path $taskGatewayState 'relay.env'),('CAMERAHUB_UDP_RELAY=host.docker.internal:'+$taskSettings.relay.port+[Environment]::NewLine+'CAMERAHUB_UDP_TOKEN='+$taskSettings.api.password+[Environment]::NewLine))
}
function Start-WindowsRelay {
    Prepare-WindowsRelay
    $taskNode=(Get-Command node -ErrorAction Stop).Source
    $taskPidFile=Join-Path $taskGatewayState 'relay-process.id'
    $taskRunning=$null;$taskProcessId=0
    if (Test-Path -LiteralPath $taskPidFile) {
        $taskSavedPid=Get-Content -LiteralPath $taskPidFile -Raw
        if ($taskSavedPid -and [int]::TryParse($taskSavedPid.Trim(),[ref]$taskProcessId)) {$taskRunning=Get-WindowsRelayIdentity $taskProcessId}
    }
    if (-not $taskRunning) {
        $taskPort=[int](Get-NativeGatewaySettings).relay.port
        if (Get-NetTCPConnection -LocalPort $taskPort -State Listen -ErrorAction SilentlyContinue) { throw 'Relay port is occupied; no second relay was started.' }
        $taskScript=Join-Path $taskGatewayRoot 'scripts/windows-udp-relay.mjs'
        $taskProcess=Start-Process -FilePath $taskNode -ArgumentList @(('"'+$taskScript+'"'),('"'+$taskGatewaySettings+'"')) -WorkingDirectory $taskGatewayRoot -WindowStyle Hidden -PassThru
        [IO.File]::WriteAllText($taskPidFile,[string]$taskProcess.Id)
    }
    for ($taskTry=0;$taskTry -lt 10;$taskTry++) {if(Test-WindowsRelay){return};Start-Sleep -Milliseconds 300}
    throw 'Camera UDP relay did not become ready.'
}
function Stop-WindowsRelay {
    $taskPidFile=Join-Path $taskGatewayState 'relay-process.id'
    $taskProcessId=0
    if (Test-Path -LiteralPath $taskPidFile) {
        $taskSavedPid=Get-Content -LiteralPath $taskPidFile -Raw
        if ($taskSavedPid -and [int]::TryParse($taskSavedPid.Trim(),[ref]$taskProcessId)) {
            if (Get-WindowsRelayIdentity $taskProcessId) {
                $taskProcess=Get-Process -Id $taskProcessId
                Stop-Process -Id $taskProcessId -Force
                if (-not $taskProcess.WaitForExit(5000)) {throw 'Relay is still stopping.'}
            }
        }
        [IO.File]::WriteAllText($taskPidFile,'')
    }
}
