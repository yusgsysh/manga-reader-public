# angie.conf.tpl
#
# Rendered at container start by deploy/docker-entrypoint.sh:
#   envsubst '${ANGIE_BACKEND_URL}' < angie.conf.tpl > angie.conf
#
# Optimizations vs. the previous revision:
#   - one `upstream` block with keepalive (connection reuse to the backend)
#   - shared proxy headers / timeouts declared once at server level
#   - locations grouped (5m list cache, 1d gallery cache, streamed image endpoints)
#   - gzip, immutable caching for hashed /assets, no-store for index.html
#   - cache stampede lock + stale-while-revalidate

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
    # Backend upstream (keepalive connection pool)
    # ============================================================

    upstream backend {
        server ${ANGIE_BACKEND_URL};
        keepalive 32;
    }

    # ============================================================
    # API Cache
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
        # Shared proxy defaults, inherited by every location that
        # proxies. A location only overrides what it needs.
        # ========================================================

        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Connection "";

        proxy_read_timeout 60s;
        proxy_send_timeout 60s;

        # Default: no caching. Cached locations opt in explicitly.
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
        }

        # ========================================================
        # Cached: gallery detail / scraped details / pages (1 day)
        #
        # GET /api/gallery/:id/:token[/details|/pages]
        # ========================================================

        location /api/gallery/ {
            proxy_pass http://backend;

            proxy_cache api_cache;
            proxy_cache_valid 200 1d;

            add_header X-Cache-Status $upstream_cache_status always;
        }

        # ========================================================
        # Streamed image proxies (Angie does not cache; backend uses MinIO)
        #
        # GET /api/thumbnail | /api/cached-thumbnail
        #     | /api/cached-image | /api/page-image
        # ========================================================

        location ~ ^/api/(thumbnail|cached-thumbnail|cached-image|page-image)$ {
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
        # All other API endpoints: pass through, never cached.
        #
        # /api/bookshelf, /api/progress, /api/recently-read,
        # /api/reading-progress/cleanup, ...
        # ========================================================

        location /api/ {
            proxy_pass http://backend;

            proxy_buffering off;
        }

        # ========================================================
        # EhTagTranslation dictionary
        # ========================================================

        location = /db.text.js {
            add_header Cache-Control "public, max-age=86400";
        }

        # ========================================================
        # Health check
        # ========================================================

        location = /healthz {
            access_log off;

            default_type text/plain;

            return 200 "ok";
        }

        # ========================================================
        # SPA
        # ========================================================

        # Vite emits content-hashed files under /assets/ -> cache forever.
        location /assets/ {
            add_header Cache-Control "public, max-age=31536000, immutable";
        }

        # The HTML shell must always be revalidated.
        location = /index.html {
            add_header Cache-Control "no-store";
        }

        location / {
            try_files $uri $uri/ /index.html;
        }
    }
}
