# Shared helpers. Never print the API URL: it contains a machine-generated secret.
$taskGatewayRoot = Split-Path -Parent $PSScriptRoot
$taskGatewayState = Join-Path $taskGatewayRoot 'data/windows-live-gateway'
$taskGatewayExe = Join-Path $taskGatewayRoot 'runtime/windows/go2rtc.exe'
$taskGatewaySettings = Join-Path $taskGatewayState 'native.json'
if (Test-Path -LiteralPath $taskGatewaySettings) {
    $taskBinaryChoice = (Get-Content -LiteralPath $taskGatewaySettings -Raw | ConvertFrom-Json).camerahub_runtime_binary
    if ($taskBinaryChoice) {
        if ($taskBinaryChoice -notin @('go2rtc.exe','go2rtc.motor-fix.exe','go2rtc.startup.exe')) { throw 'Invalid gateway binary selection.' }
        $taskGatewayExe = Join-Path $taskGatewayRoot ('runtime/windows/' + $taskBinaryChoice)
    }
}

function Get-NativeGatewaySettings {
    $taskSettings = Get-Content -LiteralPath $taskGatewaySettings -Raw | ConvertFrom-Json
    if ($taskSettings.api.listen -notmatch '^127\.0\.0\.1:179(8[4-9]|9[0-9])$' -or $taskSettings.rtsp.listen -notmatch '^127\.0\.0\.1:185(5[4-9]|6[0-9])$' -or -not $taskSettings.api.local_auth -or $taskSettings.api.username -ne 'camerahub' -or $taskSettings.api.password -notmatch '^[a-f0-9]{64}$') {
        throw 'Invalid native gateway settings. Loopback binding and local authentication are required.'
    }
    return $taskSettings
}

function Get-NativeGatewayUrl {
    $taskSettings = Get-NativeGatewaySettings
    if ($taskSettings.camerahub_runtime_mode -eq 'docker-relay') {
        return 'http://camerahub:' + $taskSettings.api.password + '@gateway:1984'
    }
    return 'http://camerahub:' + $taskSettings.api.password + '@host.docker.internal:' + $taskSettings.api.listen.Split(':')[-1]
}

function Get-NativeGatewayExecutable {
    if (-not (Test-Path -LiteralPath $taskGatewayExe)) { throw 'Build the Windows gateway first.' }
    $taskExpectedHash = (Get-Content -LiteralPath ($taskGatewayExe + '.sha256') -Raw).Trim()
    if ($taskExpectedHash -notmatch '^[a-f0-9]{64}$' -or (Get-FileHash -LiteralPath $taskGatewayExe -Algorithm SHA256).Hash.ToLowerInvariant() -ne $taskExpectedHash) { throw 'Windows gateway integrity check failed.' }
    # Immutable copies let builds update workspace files while a gateway is running.
    # Only the executable is cached here; accounts and keys stay in the workspace.
    $taskCache = Join-Path $env:LOCALAPPDATA ('CameraHub/runtime/' + $taskExpectedHash)
    New-Item -ItemType Directory -Force -Path $taskCache | Out-Null
    $taskExecutable = Join-Path $taskCache 'go2rtc.exe'
    if (-not (Test-Path -LiteralPath $taskExecutable)) { Copy-Item -LiteralPath $taskGatewayExe -Destination $taskExecutable }
    if ((Get-FileHash -LiteralPath $taskExecutable -Algorithm SHA256).Hash.ToLowerInvariant() -ne $taskExpectedHash) { throw 'Cached gateway integrity check failed.' }
    return $taskExecutable
}

function Stop-NativeGateway {
    $taskPidPath = Join-Path $taskGatewayState 'process.id'
    if (-not (Test-Path -LiteralPath $taskPidPath)) { return }
    $taskProcessId = 0
    if (-not [int]::TryParse((Get-Content -LiteralPath $taskPidPath -Raw).Trim(), [ref]$taskProcessId)) { throw 'Invalid gateway process record.' }
    $taskProcess = Get-Process -Id $taskProcessId -ErrorAction SilentlyContinue
    if ($taskProcess) {
        $taskExecutable = Get-NativeGatewayExecutable
        if ($taskProcess.Path -ne $taskExecutable -and $taskProcess.Path -ne $taskGatewayExe) { throw 'Gateway process identity does not match; no process was stopped.' }
        Stop-Process -Id $taskProcessId -Force
        if (-not $taskProcess.WaitForExit(5000)) { throw 'Windows is still holding the gateway process. Save work and restart Windows before trying again.' }
    }
    Remove-Item -LiteralPath $taskPidPath -Force
}

function Start-NativeGateway {
    $taskExecutable = Get-NativeGatewayExecutable
    $taskSettings = Get-NativeGatewaySettings
    $taskProcess = $null
    $taskPidPath = Join-Path $taskGatewayState 'process.id'
    if (Test-Path -LiteralPath $taskPidPath) {
        $taskProcessId = 0
        if ([int]::TryParse((Get-Content -LiteralPath $taskPidPath -Raw).Trim(), [ref]$taskProcessId)) {
            $taskProcess = Get-Process -Id $taskProcessId -ErrorAction SilentlyContinue
            if ($taskProcess -and $taskProcess.Path -ne $taskExecutable) { throw 'Gateway process identity does not match.' }
        }
    }
    if (-not $taskProcess) {
        $taskApiPort = [int]$taskSettings.api.listen.Split(':')[-1]
        if (Get-NetTCPConnection -LocalPort $taskApiPort -State Listen -ErrorAction SilentlyContinue) { throw 'Gateway port is in use by another process.' }
        $taskProcess = Start-Process -FilePath $taskExecutable -ArgumentList @('-config','go2rtc.yaml','-config','native.json') -WorkingDirectory $taskGatewayState -WindowStyle Hidden -PassThru
        [IO.File]::WriteAllText($taskPidPath, [string]$taskProcess.Id)
    }
    $taskBasic = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes('camerahub:' + $taskSettings.api.password))
    for ($taskAttempt=0; $taskAttempt -lt 20; $taskAttempt++) {
        if ($taskProcess.HasExited) { throw 'Native gateway stopped unexpectedly. Its private logs are in data/windows-live-gateway.' }
        try {
            $taskResponse = Invoke-WebRequest ('http://' + $taskSettings.api.listen + '/api') -UseBasicParsing -Headers @{Authorization=('Basic ' + $taskBasic)} -TimeoutSec 2
            if ($taskResponse.StatusCode -eq 200 -and ($taskResponse.Content | ConvertFrom-Json).startup -eq 'ready') { return }
        } catch { Start-Sleep -Milliseconds 300 }
    }
    throw 'Native gateway did not become ready.'
}
