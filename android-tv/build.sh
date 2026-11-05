#!/bin/sh
set -eu
if [ ! -f /keys/camerahub-tv.p12 ]; then
  keytool -genkeypair -storetype PKCS12 -keystore /keys/camerahub-tv.p12 -storepass:env KEYSTORE_PASSWORD -keypass:env KEYSTORE_PASSWORD -alias camerahub -keyalg RSA -keysize 3072 -validity 10000 -dname 'CN=CameraHub Personal TV'
fi
gradle --no-daemon testDebugUnitTest lintRelease assembleRelease
cp app/build/outputs/apk/release/app-release.apk /out/camerahub-tv-1.2.0.apk
/opt/android/build-tools/35.0.0/apksigner verify --verbose /out/camerahub-tv-1.2.0.apk
cd /out
sha256sum camerahub-tv-1.2.0.apk > camerahub-tv-1.2.0.apk.sha256
