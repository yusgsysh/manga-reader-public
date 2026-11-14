# Manga Reader — single image: frontend embedded into the Gin binary.
#
# Build (context must be the repository root):
#   docker build -t manga-reader .
#
# Run (bind to loopback; JWT login is opt-in via JWT_SECRET):
#   docker run -d -p 127.0.0.1:5173:8080 --env-file .env manga-reader
#
# Stages:
#   frontend   Bun builds the Vite production bundle
#   backend    that bundle is copied into internal/web/dist and compiled
#              into the Go binary (//go:embed), so the runtime container
#              serves API + static files from one process — no mounted
#              static files, no reverse proxy sidecar.
#
# Layer caching:
#   * manifest/lockfile copies come before source copies, so a source-only
#     change reuses the dependency layer.
#   * BuildKit cache mounts (Docker 23+ / buildx) keep Bun's download cache and
#     Go's build cache outside the image layers. They are shared between local
#     builds and self-hosted builders (a warm builder skips both downloads and
#     rebuilds). Exportable caches (`type=gha` / `type=registry`) still hold the
#     dependency layers themselves, so CI does not re-download either.

# ---- Frontend build ----
FROM oven/bun:1 AS frontend

WORKDIR /app

# Dependency layer (docker layer cache). patches/ must exist before install:
# package.json's patchedDependencies points at it, and
# `bun install --frozen-lockfile` fails without the patch files.
COPY frontend/package.json frontend/bun.lock ./
COPY frontend/patches ./patches
RUN --mount=type=cache,id=manga-bun-install,target=/root/.bun/install/cache,sharing=locked \
    bun install --frozen-lockfile

COPY frontend/ .
RUN bun run db:update

# Empty base URL → same-origin /api/*, independent of any local .env
ENV VITE_API_BASE_URL=""
RUN bun run build

# ---- Backend build ----
FROM golang:1.27-alpine AS builder

WORKDIR /src

# Module layer: no cache mount here on purpose — the downloaded modules must
# live in the layer so an exported image cache (gha / registry) restores them
# on a cold builder as well.
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .
# Drop whatever the checkout carries (placeholder or a stale local build)
# before installing the bundle from the frontend stage.
RUN rm -rf internal/web/dist
COPY --from=frontend /app/dist ./internal/web/dist

# Build static binary (modernc.org/sqlite is pure-Go, no CGO needed).
# The Go build cache is a mount: warm builders skip recompiling stdlib and
# unchanged packages, and it never bloats the layer cache. GOCACHE is
# concurrency-safe (content-addressed + atomic writes), so it is left at the
# default sharing=shared — locked would serialize parallel builds that share
# one builder.
RUN --mount=type=cache,id=manga-go-build,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/manga-reader . \
    && CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/reset-admin ./cmd/reset-admin

# ---- Runtime stage ----
FROM alpine:3.22

# CA certificates for HTTPS requests to ExHentai, tzdata for time handling
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 appuser

WORKDIR /app
COPY --from=builder /out/manga-reader /app/manga-reader
COPY --from=builder /out/reset-admin /app/reset-admin

RUN mkdir -p /app/data && chown -R appuser:appuser /app

USER appuser

ENV EHENTAI_PORT=:8080
ENV MANGA_READER_DB_PATH=/app/data/manga-reader.db

VOLUME ["/app/data"]

EXPOSE 8080

# /healthz is deliberately exempt from JWT so container healthchecks work.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/manga-reader"]
