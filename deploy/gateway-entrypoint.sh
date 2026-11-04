#!/bin/sh
set -eu
umask 077
mkdir -p /config
if [ ! -f /config/go2rtc.yaml ]; then
  cat > /config/go2rtc.yaml <<EOF
api:
  listen: "${GATEWAY_LISTEN:-:1984}"
  allow_paths:
    - /api
    - /api/streams
    - /api/ws
    - /api/webrtc
    - /api/stream.m3u8
    - /api/hls/playlist.m3u8
    - /api/hls/init.mp4
    - /api/hls/segment.m4s
    - /api/hls/segment.ts
    - /api/xiaomi
    - /api/xiaomi/command
rtsp:
  listen: "127.0.0.1:8554"
webrtc:
  listen: ":8555"
log:
  level: error
ffmpeg:
  demo_loop: "-re -stream_loop -1 -i {input}"
streams:
  _bootstrap: []
EOF
fi
chown -R 10001:10001 /config
chmod 700 /config
chmod 600 /config/go2rtc.yaml
exec su-exec 10001:10001 /usr/local/bin/go2rtc -config /config/go2rtc.yaml "$@"
