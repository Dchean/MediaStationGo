# syntax=docker/dockerfile:1.6
# =============================================================================
# Multi-architecture build for MediaStationGo.
#
# Stage 1 (frontend) :  Node 20  -> static SPA bundle
# Stage 2 (backend)  :  Go 1.25  -> single static binary (CGO_ENABLED=0)
# Stage 3 (runtime)  :  Alpine 3.19 -> ffmpeg + tzdata + non-root user
#
# Build:
#   docker buildx build --platform linux/amd64,linux/arm64 \
#     -t mediastation-go:latest --push .
# =============================================================================

# ---- Stage 1: frontend (always build on the host architecture) -------------
FROM --platform=$BUILDPLATFORM node:20-alpine AS frontend
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# ---- Stage 2: backend (cross-compiled to TARGETPLATFORM) -------------------
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS backend
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o mediastation-go ./cmd/server

# ---- Stage 3: runtime ------------------------------------------------------
FROM alpine:3.19
# ffmpeg plus optional VAAPI packages. Intel media driver is x86_64-only.
# NVENC requires the proprietary NVIDIA Container Toolkit on the host —
# no extra packages needed inside the image, only --gpus all on docker run.
RUN apk add --no-cache \
        ffmpeg \
        tzdata \
        ca-certificates \
        wget \
        su-exec \
    && if [ "$(apk --print-arch)" = "x86_64" ]; then \
        apk add --no-cache intel-media-driver libva-utils mesa-va-gallium; \
    else \
        apk add --no-cache libva-utils mesa-va-gallium || true; \
    fi \
    && rm -rf /var/cache/apk/*

# Non-root user for the long-running process.
RUN addgroup -S mediastation && adduser -S mediastation -G mediastation

WORKDIR /app
COPY --from=backend /app/mediastation-go /usr/local/bin/mediastation-go
COPY --from=frontend /app/web/dist /app/web/dist

RUN mkdir -p /data /cache /media \
    && chown -R mediastation:mediastation /data /cache /media

# Default environment (overridable via docker-compose / `docker run -e`).
ENV MEDIASTATION_APP_PORT=8080 \
    MEDIASTATION_APP_DATA_DIR=/data \
    MEDIASTATION_APP_WEB_DIR=/app/web/dist \
    MEDIASTATION_DATABASE_DB_PATH=/data/mediastation.db \
    MEDIASTATION_CACHE_CACHE_DIR=/cache \
    MEDIASTATION_LOGGING_LEVEL=info \
    TZ=Asia/Shanghai

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:8080/api/health || exit 1

# Tiny entrypoint that lets us swap to a different UID/GID via PUID/PGID
# (handy on NAS deployments where bind-mounted volumes belong to a non-root
# user). When PUID == 0 we skip su-exec entirely and run as root.
RUN printf '#!/bin/sh\n\
PUID=${PUID:-$(id -u mediastation)}\n\
PGID=${PGID:-$(id -g mediastation)}\n\
if [ "$PUID" != "$(id -u mediastation)" ] || [ "$PGID" != "$(id -g mediastation)" ]; then\n\
  deluser mediastation 2>/dev/null || true\n\
  delgroup mediastation 2>/dev/null || true\n\
  addgroup -g "$PGID" -S mediastation\n\
  adduser -u "$PUID" -G mediastation -S mediastation\n\
fi\n\
chown -R mediastation:mediastation /data /cache 2>/dev/null || true\n\
chown mediastation:mediastation /media 2>/dev/null || true\n\
if [ "$PUID" = "0" ]; then\n\
  exec mediastation-go\n\
fi\n\
exec su-exec mediastation mediastation-go\n' > /entrypoint.sh \
    && chmod +x /entrypoint.sh

CMD ["/entrypoint.sh"]
