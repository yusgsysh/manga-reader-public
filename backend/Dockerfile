# ---- Build stage ----
FROM golang:1.27-alpine AS builder

WORKDIR /src

# Cache module dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build static binary (modernc.org/sqlite is pure-Go, no CGO needed)
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/manga-reader .

# ---- Runtime stage ----
FROM alpine:3.21

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
