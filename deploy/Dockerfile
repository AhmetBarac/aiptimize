# syntax=docker/dockerfile:1

# ---- web build -------------------------------------------------------------
FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- go build --------------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist

ARG VERSION=dev
ARG COMMIT=
ARG BUILD_DATE=
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o /out/aiptimize ./cmd/aiptimize

# ---- runtime ---------------------------------------------------------------
FROM alpine:3.22

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
