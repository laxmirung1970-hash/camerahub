FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
RUN apk add --no-cache git
WORKDIR /build
# Official source and community patch are pinned, reviewed and built locally.
RUN git init && git remote add origin https://github.com/AlexxIT/go2rtc.git && git fetch --depth 1 origin b5948cfb25404cc5cb37b166ecaa2dca20b11d4b && git checkout FETCH_HEAD
COPY vendor/go2rtc-xiaomi-control.patch /tmp/control.patch
RUN echo 'd18cb4736073c8466e84109bd4f030444e5ed7ce8a7c81bee6f8034c59d7c4d7  /tmp/control.patch' | sha256sum -c - && git apply /tmp/control.patch
COPY vendor/xiaomi-motor-replies.patch /tmp/motor.patch
RUN echo 'c26552d55cf0752f483bac8eb5c12a63d6dd41292c26c87a78fcf437a7f673e9  /tmp/motor.patch' | sha256sum -c - && git apply /tmp/motor.patch && go test ./pkg/xiaomi/miss
COPY vendor/gateway-startup.patch /tmp/startup.patch
RUN echo '6a90e7382bc89f0ff2d960f8c11b955f95dc0af051172a4acfd37ada5842ee4a  /tmp/startup.patch' | sha256sum -c - && git apply /tmp/startup.patch
COPY vendor/windows-udp-relay.patch /tmp/relay.patch
RUN echo '708038dbcf3aed4fc7fb350ee8af380dd357d2d7468ee06c9323b89e083c5490  /tmp/relay.patch' | sha256sum -c - && git apply /tmp/relay.patch && go test ./pkg/xiaomi/miss/cs2
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /go2rtc .

FROM alpine:3.22 AS target-base
FROM --platform=$BUILDPLATFORM alpine:3.22 AS runtime-files
ARG TARGETARCH
COPY --from=target-base / /rootfs/
RUN case "$TARGETARCH" in arm64) task_arch=aarch64;; amd64) task_arch=x86_64;; *) exit 1;; esac; \
    apk --root /rootfs --arch "$task_arch" --initdb --no-cache --no-scripts add alpine-baselayout busybox ffmpeg ca-certificates-bundle su-exec && \
    for task_tool in sh chmod chown mkdir wget cat rm sed ls sleep env; do ln -sf busybox /rootfs/bin/$task_tool; done && \
    mkdir -p /rootfs/tmp && chmod 1777 /rootfs/tmp
FROM scratch
COPY --from=runtime-files /rootfs/ /
COPY --from=build /go2rtc /usr/local/bin/go2rtc
COPY --chmod=755 deploy/gateway-entrypoint.sh /entrypoint.sh
ENV GATEWAY_LISTEN=:1984
ENTRYPOINT ["/entrypoint.sh"]
