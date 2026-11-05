# Installs the newest releases/camerahub-tv-*.apk on an Android TV / Mi TV Stick over Wi-Fi.
# The TV needs Developer options -> USB debugging on. adb runs inside the Android builder image,
# and its key is kept in a Docker volume so "Always allow" on the TV is asked only once.
param([string]$TvIp)
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
$taskImage = 'camerahub/android-builder:1'
docker info --format '{{.ServerVersion}}' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'เปิด Docker Desktop ก่อน แล้วลองใหม่' }
if (-not (docker image ls -q $taskImage)) { throw 'ไม่พบเครื่องมือ adb (image camerahub/android-builder:1) ให้รัน scripts/Build-TV.ps1 หนึ่งครั้ง' }
$taskApk = Get-ChildItem -LiteralPath (Join-Path $taskRoot 'releases') -Filter 'camerahub-tv-*.apk' | Sort-Object LastWriteTime | Select-Object -Last 1
if (-not $taskApk) { throw 'ไม่พบไฟล์ releases/camerahub-tv-*.apk' }
if (-not $TvIp) { $TvIp = (Read-Host 'พิมพ์ IP ของทีวี (ตัวเลขแบบ 192.168.x.x) แล้วกด Enter').Trim() }
if ($TvIp -notmatch '^(10|172|192)\.\d{1,3}\.\d{1,3}\.\d{1,3}$') { throw 'IP ไม่ถูกต้อง ต้องเป็นตัวเลข 4 ชุด เช่น 192.168.x.x' }
Write-Host ('กำลังส่ง ' + $taskApk.Name + ' ไปที่ทีวี...')
Write-Host 'ถ้าทีวีขึ้นหน้าต่าง "Allow USB debugging?" ให้ติ๊ก Always allow แล้วกด OK (รอได้ 90 วินาที)'
$taskTarget = $TvIp + ':5555'
# Connect, wait until the TV accepts this computer, install over any older copy, then open the app.
$taskSteps = @"
adb start-server >/dev/null 2>&1
reach=0
for n in 1 2 3; do
  case "`$(timeout 6 adb connect $taskTarget 2>&1)" in *connected*) reach=1; break;; esac
  sleep 1
done
[ `$reach = 1 ] || { echo NOT_REACHABLE; exit 2; }
i=0
while [ `$i -lt 45 ]; do
  state=`$(timeout 4 adb -s $taskTarget get-state 2>/dev/null)
  [ "`$state" = device ] && break
  [ `$((i % 10)) = 9 ] && { adb disconnect $taskTarget >/dev/null 2>&1; timeout 6 adb connect $taskTarget >/dev/null 2>&1; }
  sleep 2; i=`$((i+1))
done
[ "`$state" = device ] || { echo NOT_AUTHORIZED; exit 3; }
adb -s $taskTarget install -r '/apk/$($taskApk.Name)' || exit 4
adb -s $taskTarget shell monkey -p local.camerahub.tv -c android.intent.category.LEANBACK_LAUNCHER 1 >/dev/null 2>&1
echo INSTALLED
"@
# Script goes in on stdin: Windows PowerShell strips double quotes from native-command arguments.
$taskOutput = ($taskSteps -replace "`r", '') | docker run --rm -i --mount type=volume,source=camerahub-adb-keys,target=/root/.android --mount ("type=bind,source=" + (Join-Path $taskRoot 'releases') + ",target=/apk,readonly") $taskImage sh -s
$taskCode = $LASTEXITCODE
$taskOutput | ForEach-Object { Write-Host $_ }
if ($taskCode -eq 0) { Write-Host 'สำเร็จ: ติดตั้ง CameraHub TV แล้ว แอปเปิดบนทีวี'; exit 0 }
if ($taskCode -eq 2) { throw 'ติดต่อทีวีไม่ได้: ตรวจ IP, ให้ทีวีกับคอมใช้ Wi-Fi บ้านเดียวกัน, และเปิด USB debugging บนทีวีแล้ว' }
if ($taskCode -eq 3) { throw 'ทีวีไม่ยอมรับคอมเครื่องนี้: ตรวจ IP, ให้ทีวีกับคอมใช้ Wi-Fi บ้านเดียวกัน, เปิด USB debugging แล้วกด Allow บนทีวี' }
throw 'ติดตั้งไม่สำเร็จ (ดูข้อความด้านบน)'
