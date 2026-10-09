# Manga Reader — single image: frontend embedded into the Gin binary.
#
# Build (context must be the repository root):
#   docker build -t manga-reader .
#
# Run:
#   docker run -d -p 5173:8080 --env-file .env manga-reader
#
# Stages:
#   frontend   Bun builds the Vite production bundle
#   backend    that bundle is copied into internal/web/dist and compiled
#              into the Go binary (//go:embed), so the runtime container
#              serves API + static files from one process — no mounted
#              static files, no reverse proxy sidecar.

# ---- Frontend build ----
FROM oven/bun:1 AS frontend

WORKDIR /app

# Dependency layer (docker layer cache). patches/ must exist before install:
# package.json's patchedDependencies points at it, and
# `bun install --frozen-lockfile` fails without the patch files.
COPY frontend/package.json frontend/bun.lock ./
COPY frontend/patches ./patches
RUN bun install --frozen-lockfile

COPY frontend/ .
RUN bun run db:update

# Empty base URL → same-origin /api/*, independent of any local .env
ENV VITE_API_BASE_URL=""
RUN bun run build

# ---- Backend build ----
FROM golang:1.27-alpine AS builder

WORKDIR /src

# Cache module dependencies
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .
# Drop whatever the checkout carries (placeholder or a stale local build)
# before installing the bundle from the frontend stage.
RUN rm -rf internal/web/dist
COPY --from=frontend /app/dist ./internal/web/dist

# Build static binary (modernc.org/sqlite is pure-Go, no CGO needed)
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/manga-reader .

# ---- Runtime stage ----
FROM alpine:latest

# CA certificates for HTTPS requests to ExHentai, tzdata for time handling
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 appuser

WORKDIR /app
COPY --from=builder /out/manga-reader /app/manga-reader

RUN mkdir -p /app/data && chown -R appuser:appuser /app

USER appuser

ENV EHENTAI_PORT=:8080
ENV MANGA_READER_DB_PATH=/app/data/manga-reader.db

VOLUME ["/app/data"]

EXPOSE 8080

ENTRYPOINT ["/app/manga-reader"]
