FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /build
COPY web/package*.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS backend
WORKDIR /build
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /camerahub .

FROM alpine:3.22 AS target-base
FROM --platform=$BUILDPLATFORM alpine:3.22 AS runtime-files
ARG TARGETARCH
COPY --from=target-base / /rootfs/
# Assemble target packages without executing ARM binaries on the Windows build host.
RUN case "$TARGETARCH" in arm64) task_arch=aarch64;; amd64) task_arch=x86_64;; *) exit 1;; esac; \
    apk --root /rootfs --arch "$task_arch" --initdb --no-cache --no-scripts add alpine-baselayout busybox ca-certificates-bundle su-exec && \
    for task_tool in sh chmod chown mkdir wget cat rm sed ls sleep env; do ln -sf busybox /rootfs/bin/$task_tool; done && \
    mkdir -p /rootfs/tmp && chmod 1777 /rootfs/tmp
FROM scratch
COPY --from=runtime-files /rootfs/ /
COPY --from=backend /camerahub /usr/local/bin/camerahub
COPY --from=web /build/dist /web
# TV self-update APK from scripts/Publish-TVUpdate.ps1; README only when none was published.
COPY tv-update/ /tv-update/
COPY --chmod=755 deploy/app-entrypoint.sh /entrypoint.sh
ENV APP_DATA=/data APP_WEB=/web APP_LISTEN=:8080
EXPOSE 8080
ENTRYPOINT ["/entrypoint.sh"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s CMD wget -q -O /dev/null http://127.0.0.1:8080/api/health || exit 1
