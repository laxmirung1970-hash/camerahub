# Registers (or with -Uninstall removes) a per-user scheduled task that runs Watch-Live.ps1
# at logon and every 5 minutes. No admin rights; runs only while this user is logged on.
param([switch]$Uninstall)
$ErrorActionPreference = 'Stop'
$taskName = 'CameraHub Live Watchdog'
if ($Uninstall) {
    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue
    Write-Host 'Watchdog task removed.'
    exit 0
}
$taskScript = Join-Path $PSScriptRoot 'Watch-Live.ps1'
$taskUser = [Security.Principal.WindowsIdentity]::GetCurrent().Name
# conhost --headless keeps the 5-minute run from flashing a console window.
$taskAction = New-ScheduledTaskAction -Execute (Join-Path $env:SystemRoot 'System32\conhost.exe') -Argument ('--headless powershell.exe -NoProfile -ExecutionPolicy Bypass -File "' + $taskScript + '"') -WorkingDirectory (Split-Path -Parent $PSScriptRoot)
$taskTriggers = @(
    (New-ScheduledTaskTrigger -AtLogOn -User $taskUser),
    (New-ScheduledTaskTrigger -Once -At (Get-Date).AddMinutes(1) -RepetitionInterval (New-TimeSpan -Minutes 5))
)
$taskPrincipal = New-ScheduledTaskPrincipal -UserId $taskUser -LogonType Interactive -RunLevel Limited
$taskSettings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -MultipleInstances IgnoreNew -ExecutionTimeLimit (New-TimeSpan -Minutes 3)
Register-ScheduledTask -TaskName $taskName -Action $taskAction -Trigger $taskTriggers -Principal $taskPrincipal -Settings $taskSettings -Description 'Restarts the CameraHub camera UDP relay if it stops while live is running.' -Force | Out-Null
Write-Host 'Watchdog task installed: checks the camera relay at logon and every 5 minutes.'
Write-Host 'Log (only when it acts): data/windows-live-gateway/watchdog.log'
