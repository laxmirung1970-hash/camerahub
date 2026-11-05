# Put the newest built TV APK into tv-update/, which the app image bundles and
# serves to paired TVs at /api/tv/update (backend/tv_update.go). Run after
# Build-TV.ps1 (it calls this), then rebuild the app image to offer it.
param([string]$Apk = '')
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
$taskMeta = Join-Path $taskRoot 'android-tv/app/build/outputs/apk/release/output-metadata.json'
if (-not (Test-Path -LiteralPath $taskMeta)) { throw 'Build the TV APK first (scripts/Build-TV.ps1).' }
$taskBuild = (Get-Content -Raw -LiteralPath $taskMeta | ConvertFrom-Json)
$taskElement = $taskBuild.elements | Select-Object -First 1
if ($taskBuild.applicationId -ne 'local.camerahub.tv' -or [int64]$taskElement.versionCode -le 0) { throw 'Unexpected TV build metadata.' }
$taskName = [string]$taskElement.versionName
if ($taskName -notmatch '^[0-9A-Za-z.+-]{1,32}$') { throw "Unexpected TV version name: $taskName" }
if (-not $Apk) { $Apk = Join-Path $taskRoot "releases/camerahub-tv-$taskName.apk" }
if (-not (Test-Path -LiteralPath $Apk)) { throw "Missing $Apk" }
# The signed release next to its .sha256, not an arbitrary file.
$taskHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $Apk).Hash.ToLowerInvariant()
$taskExpected = ((Get-Content -Raw -LiteralPath "$Apk.sha256") -split '\s+')[0].ToLowerInvariant()
if ($taskHash -ne $taskExpected) { throw "$Apk does not match its .sha256" }
$taskBuilt = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path (Split-Path -Parent $taskMeta) $taskElement.outputFile)).Hash.ToLowerInvariant()
if ($taskHash -ne $taskBuilt) { throw "$Apk is not the APK described by the last build ($taskName)" }
$taskOut = Join-Path $taskRoot 'tv-update'
New-Item -ItemType Directory -Force -Path $taskOut | Out-Null
Copy-Item -LiteralPath $Apk -Destination (Join-Path $taskOut 'camerahub-tv.apk') -Force
$taskJson = @{ versionCode = [int64]$taskElement.versionCode; versionName = $taskName } | ConvertTo-Json -Compress
[IO.File]::WriteAllText((Join-Path $taskOut 'camerahub-tv.json'), $taskJson, (New-Object Text.UTF8Encoding $false))
Write-Host "TV update ready: $taskName (versionCode $($taskElement.versionCode)) sha256 $taskHash"
Write-Host 'Rebuild the app image to offer it: docker build -t camerahub/app:1.0.0-amd64 .'
