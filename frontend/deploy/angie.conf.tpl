# angie.conf.tpl
#
# Rendered at container start by deploy/docker-entrypoint.sh:
#   generate-auth.sh            (writes /etc/angie/basic-auth.conf first)
#   envsubst '${ANGIE_BACKEND_URL}' < angie.conf.tpl > angie.conf
#
# Optimizations vs. the previous revision:
#   - one `upstream` block with keepalive (connection reuse to the backend)
#   - shared proxy headers / timeouts declared once at server level
#   - locations grouped (5m list cache, streamed gallery pages/images)
#   - gzip, immutable caching for hashed /assets, no-store for index.html
#   - cache stampede lock + stale-while-revalidate
#   - HTTP Basic Auth for the whole app via generated include
#     (/etc/angie/basic-auth.conf; comment-only when disabled) + per-IP
#     rate limit; /healthz exempt. No CSP on purpose — see README.

user angie;
worker_processes auto;

error_log /dev/stderr notice;

events {
    worker_connections 1024;
}

http {
    include /etc/angie/mime.types;
    default_type application/octet-stream;

    access_log /dev/stdout;
    server_tokens off;

    sendfile on;
    sendfile_max_chunk 1m;
    tcp_nopush on;
    tcp_nodelay on;
    keepalive_timeout 65;
    types_hash_max_size 2048;

    open_file_cache max=1000 inactive=20s;
    open_file_cache_valid 30s;
    open_file_cache_min_uses 2;
    open_file_cache_errors on;

    # ============================================================
    # Compression (gzip + brotli + zstd, negotiated via
    # Accept-Encoding; whichever filter runs first wins)
    # ============================================================

    gzip on;
    gzip_vary on;
    gzip_proxied any;
    gzip_comp_level 6;
    gzip_min_length 1024;
    gzip_types
        text/plain
        text/css
        text/javascript
        application/javascript
        application/json
        application/xml
        image/svg+xml;

    brotli on;
    brotli_comp_level 5;
    brotli_min_length 1024;
    brotli_types
        text/plain
        text/css
        text/javascript
        application/javascript
        application/json
        application/xml
        image/svg+xml;

    zstd on;
    zstd_comp_level 5;
    zstd_min_length 1024;
    zstd_types
        text/plain
        text/css
        text/javascript
        application/javascript
        application/json
        application/xml
        image/svg+xml;

    # ============================================================
    # Per-IP request rate limit (basic flood protection for the
    # Basic Auth entry point).
    #
    # limit_req_zone is http-level, so the zone is declared here;
    # the `limit_req` directive itself lives in the generated
    # /etc/angie/basic-auth.conf, i.e. it only takes effect when
    # Basic Auth is enabled — dev runs stay untouched.
    # ============================================================

    limit_req_zone $binary_remote_addr zone=auth_limit:10m rate=50r/s;
    limit_req_status 429;

    # ============================================================
    # Backend upstream (keepalive connection pool)
    # ============================================================

    upstream backend {
        server ${ANGIE_BACKEND_URL};
        keepalive 32;
    }

    # ============================================================
    # API Cache (list endpoints only)
    # ============================================================

    proxy_cache_path /var/cache/angie/api
        levels=1:2
        keys_zone=api_cache:10m
        max_size=512m
        inactive=60m
        use_temp_path=off;

    server {
        listen 80;
        server_name _;

        root /usr/share/angie/html;
        index index.html;

        # ========================================================
        # HTTP Basic Authentication (public deployments).
        #
        # deploy/generate-auth.sh writes /etc/angie/basic-auth.conf
        # at container start:
        #   enabled  -> auth_basic + auth_basic_user_file + limit_req
        #   disabled -> comment only (no behaviour change)
        # Server-level, so it is inherited by every location below —
        # SPA, /api/*, SSE, images, assets. /healthz opts out.
        # ========================================================

        include /etc/angie/basic-auth.conf;

        # ========================================================
        # Security headers. No Content-Security-Policy on purpose:
        # a strict CSP risks breaking React, SSE, the image proxy
        # and the reader. TLS / HSTS belong to the HTTPS layer in
        # front of this proxy (see README 公网部署).
        # ========================================================

        add_header X-Content-Type-Options nosniff always;
        add_header X-Frame-Options SAMEORIGIN always;
        add_header Referrer-Policy strict-origin-when-cross-origin always;

        # ========================================================
        # Shared proxy defaults, inherited by every location that
        # proxies. A location only overrides what it needs.
        # ========================================================

        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Connection "";

        # Basic Auth is terminated here; the proxy password must never
        # reach the backend or its logs (the app only ever needs the
        # X-Sync-Token header for sync). An empty value removes the
        # header from the proxied request.
        proxy_set_header Authorization "";

        proxy_read_timeout 60s;
        proxy_send_timeout 60s;

        # Default: no caching. Only the list endpoints opt in below. Gallery
        # metadata/details are not cached here (the backend owns gallery_cache).
        proxy_cache off;

        # Cache stampede protection + stale-while-revalidate.
        proxy_cache_lock on;
        proxy_cache_background_update on;
        proxy_cache_use_stale error timeout updating http_500 http_502 http_503 http_504;

        # ========================================================
        # Cached: list endpoints (5 minutes)
        #
        # GET /api/galleries | /api/search | /api/popular | /api/watched
        # ========================================================

        location ~ ^/api/(galleries|search|popular|watched)$ {
            proxy_pass http://backend;

            proxy_cache api_cache;
            proxy_cache_valid 200 5m;

            add_header X-Cache-Status $upstream_cache_status always;
            # add_header at this level drops the inherited security
            # headers — repeat them here (same for the other
            # locations below that define their own add_header).
            add_header X-Content-Type-Options nosniff always;
            add_header X-Frame-Options SAMEORIGIN always;
            add_header Referrer-Policy strict-origin-when-cross-origin always;
        }

        # ========================================================
        # Streamed: gallery page list (NDJSON)
        #
        # GET /api/gallery/:id/:token/pages
        #
        # Kept apart from the default /api/ location only for the longer
        # timeouts; the incremental stream is never buffered or cached.
        # ========================================================

        location ~ ^/api/gallery/[^/]+/[^/]+/pages$ {
            proxy_pass http://backend;

            proxy_buffering off;
            proxy_read_timeout 300s;
            proxy_send_timeout 300s;
        }

        # ========================================================
        # Streamed image proxies (Angie does not cache; backend uses MinIO)
        #
        # GET /api/image/{page,thumbnail,page-thumbnail}
        #     /api/image-cache/{page,thumbnail,page-thumbnail}
        # ========================================================

        location ^~ /api/image/ {
            proxy_pass http://backend;

            proxy_buffering off;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
        }

        location ^~ /api/image-cache/ {
            proxy_pass http://backend;

            proxy_buffering off;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
        }

        # ========================================================
        # Prefill download tasks (streaming ZIP; slow backfills)
        #
        # /api/prefill/*
        # ========================================================

        location /api/prefill {
            proxy_pass http://backend;

            proxy_buffering off;
            proxy_read_timeout 600s;
            proxy_send_timeout 600s;
        }

        # ========================================================
        # SSE change streams (peer sync + local cache invalidation)
        #
        # GET /api/sync/events | /api/events/changes
        #
        # Long-lived connections: buffering off so events flush
        # immediately, a generous read timeout on top of the backend's
        # 25s heartbeats, never cached or compressed.
        # ========================================================

        location ~ ^/api/(sync/events|events/changes)$ {
            proxy_pass http://backend;

            proxy_buffering off;
            proxy_cache off;
            gzip off;
            proxy_read_timeout 3600s;
            proxy_send_timeout 3600s;
        }

        # ========================================================
        # All other API endpoints: pass through, never cached.
        #
        # /api/bookshelf, /api/progress, /api/recently-read,
        # /api/reading-progress/cleanup, ...
        # ========================================================

        location /api/ {
            proxy_pass http://backend;

            proxy_buffering off;
        }

        # =========================================================
        # Sync push: snapshots of gallery_cache rows (pages, tags,
        # thumbnail geometry) can exceed the default 1m
        # client_max_body_size, so give this endpoint headroom.
        # ========================================================

        location = /api/sync/push {
            client_max_body_size 64m;

            proxy_pass http://backend;

            proxy_buffering off;
        }

        # ========================================================
        # EhTagTranslation dictionary
        # ========================================================

        location = /db.text.js {
            add_header Cache-Control "public, max-age=86400";
            add_header X-Content-Type-Options nosniff always;
            add_header X-Frame-Options SAMEORIGIN always;
            add_header Referrer-Policy strict-origin-when-cross-origin always;
        }

        # ========================================================
        # Health check — exempt from Basic Auth so Docker healthchecks
        # and external monitors keep working without credentials.
        # The response is a static "ok": no backend data, no cookie,
        # no credentials, no config.
        # ========================================================

        location = /healthz {
            access_log off;

            auth_basic off;

            default_type text/plain;

            return 200 "ok";
        }

        # ========================================================
        # SPA
        # ========================================================

        # Vite emits content-hashed files under /assets/ -> cache forever.
        location /assets/ {
            add_header Cache-Control "public, max-age=31536000, immutable";
            add_header X-Content-Type-Options nosniff always;
            add_header X-Frame-Options SAMEORIGIN always;
            add_header Referrer-Policy strict-origin-when-cross-origin always;
        }

        # The HTML shell must always be revalidated.
        location = /index.html {
            add_header Cache-Control "no-store";
            add_header X-Content-Type-Options nosniff always;
            add_header X-Frame-Options SAMEORIGIN always;
            add_header Referrer-Policy strict-origin-when-cross-origin always;
        }

        location / {
            try_files $uri $uri/ /index.html;
        }
    }
}
