# syntax=docker/dockerfile:1

# Builder stages run on the native build platform and cross-compile for the
# target. Only the runtime stage is target-platform, so an arm64 image can be
# produced on an amd64 workstation without QEMU emulation.

# ---- web build -------------------------------------------------------------
FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- go build --------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=
ARG BUILD_DATE=
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o /out/aiptimize ./cmd/aiptimize

# ---- runtime ---------------------------------------------------------------
FROM alpine:3.22

ARG VERSION=dev
ARG COMMIT=
ARG BUILD_DATE=
ARG NOTES=

# Read back by the in-app update check, so a newer build can describe itself
# before it is pulled.
LABEL org.opencontainers.image.title="aiptimize" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source="https://github.com/AhmetBarac/aiptimize" \
      com.aiptimize.notes="${NOTES}"

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 -S aiptimize \
    && adduser -u 10001 -S -G aiptimize -h /app aiptimize \
    && mkdir -p /data /app \
    && chown -R aiptimize:aiptimize /data /app

COPY --from=build /out/aiptimize /app/aiptimize

USER 10001
EXPOSE 8080
VOLUME ["/data"]

ENV AIPTIMIZE_DATA_DIR=/data \
    AIPTIMIZE_LISTEN=0.0.0.0:8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app/aiptimize", "healthcheck"]

ENTRYPOINT ["/app/aiptimize"]
CMD ["serve"]
