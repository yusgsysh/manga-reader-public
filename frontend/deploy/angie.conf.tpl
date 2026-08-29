# angie.conf.tpl

user angie;
worker_processes auto;

error_log /dev/stderr notice;

events {
    worker_connections 1024;
}

http {
    include /etc/angie/mime.types;
    default_type application/octet-stream;

    sendfile on;

    # ============================================================
    # API Cache
    # ============================================================

    proxy_cache_path /var/cache/angie/api
        levels=1:2
        keys_zone=api_cache:10m
        max_size=512m
        inactive=30m
        use_temp_path=off;


    server {
        listen 80;
        server_name _;

        root /usr/share/angie/html;
        index index.html;


        # ========================================================
        # Homepage Gallery List
        #
        # GET /api/gallerys?page=0
        #
        # Cache: 5 minutes
        # ========================================================

        location = /api/gallerys {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;

            proxy_cache api_cache;
            proxy_cache_methods GET HEAD;

            proxy_cache_valid 200 5m;

            add_header X-Cache-Status $upstream_cache_status always;

            proxy_read_timeout 60s;
        }


        # ========================================================
        # Search
        #
        # GET /api/search?q=xxx&page=0
        #
        # Cache: 5 minutes
        # ========================================================

        location = /api/search {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;

            proxy_cache api_cache;
            proxy_cache_methods GET HEAD;

            proxy_cache_valid 200 5m;

            add_header X-Cache-Status $upstream_cache_status always;

            proxy_read_timeout 60s;
        }


        # ========================================================
        # Popular
        #
        # GET /api/popular?page=0
        #
        # Cache: 10 minutes
        # ========================================================

        location = /api/popular {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;

            proxy_cache api_cache;
            proxy_cache_methods GET HEAD;

            proxy_cache_valid 200 10m;

            add_header X-Cache-Status $upstream_cache_status always;

            proxy_read_timeout 60s;
        }


        # ========================================================
        # Watched / Subscription
        #
        # GET /api/watched?page=0
        #
        # Cache: 5 minutes
        # ========================================================

        location = /api/watched {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;

            proxy_cache api_cache;
            proxy_cache_methods GET HEAD;

            proxy_cache_valid 200 5m;

            add_header X-Cache-Status $upstream_cache_status always;

            proxy_read_timeout 60s;
        }


        # ========================================================
        # Gallery Detail
        #
        # GET /api/gallery/:id/:token
        #
        # Cache: 30 minutes
        # ========================================================

        location ~ ^/api/gallery/[0-9]+/[^/]+$ {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;

            proxy_cache api_cache;
            proxy_cache_methods GET HEAD;

            proxy_cache_valid 200 30m;

            add_header X-Cache-Status $upstream_cache_status always;

            proxy_read_timeout 60s;
        }


        # ========================================================
        # Gallery Details (Scraped)
        #
        # GET /api/gallery/:id/:token/details
        #
        # Cache: 30 minutes
        # ========================================================

        location ~ ^/api/gallery/[0-9]+/[^/]+/details$ {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;

            proxy_cache api_cache;
            proxy_cache_methods GET HEAD;

            proxy_cache_valid 200 30m;

            add_header X-Cache-Status $upstream_cache_status always;

            proxy_read_timeout 60s;
        }


        # ========================================================
        # Gallery Pages
        #
        # GET /api/gallery/:id/:token/pages
        #
        # Cache: 30 minutes
        # ========================================================

        location ~ ^/api/gallery/[0-9]+/[^/]+/pages$ {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;

            proxy_cache api_cache;
            proxy_cache_methods GET HEAD;

            proxy_cache_valid 200 30m;

            add_header X-Cache-Status $upstream_cache_status always;

            proxy_read_timeout 60s;
        }


        # ========================================================
        # Thumbnail
        #
        # GET /api/thumbnail?url=...
        #
        # 不使用 Angie Cache
        # ========================================================

        location = /api/thumbnail {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;

            proxy_no_cache 1;
            proxy_cache_bypass 1;

            proxy_buffering off;

            proxy_read_timeout 60s;
            proxy_send_timeout 60s;
        }


        # ========================================================
        # Cached Thumbnail
        #
        # GET /api/cached-thumbnail?url=...
        #
        # Angie 不缓存
        # Backend -> MinIO
        # ========================================================

        location = /api/cached-thumbnail {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;

            proxy_no_cache 1;
            proxy_cache_bypass 1;

            proxy_buffering off;

            proxy_read_timeout 60s;
            proxy_send_timeout 60s;
        }


        # ========================================================
        # Cached Image
        #
        # GET /api/cached-image?url=...
        #
        # Angie 不缓存
        # Backend -> MinIO
        # ========================================================

        location = /api/cached-image {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;

            proxy_no_cache 1;
            proxy_cache_bypass 1;

            proxy_buffering off;

            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
        }


        # ========================================================
        # Page Image
        #
        # GET /api/page-image?url=...
        #
        # 不缓存
        # ========================================================

        location = /api/page-image {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_no_cache 1;
            proxy_cache_bypass 1;

            proxy_buffering off;

            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
        }


        # ========================================================
        # Bookshelf
        #
        # GET /api/bookshelf
        # POST /api/bookshelf/:id/:token
        # DELETE /api/bookshelf/:id/:token
        #
        # 不缓存
        # ========================================================

        location /api/bookshelf {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_no_cache 1;
            proxy_cache_bypass 1;

            proxy_read_timeout 60s;
        }


        # ========================================================
        # Reading Progress
        #
        # GET /api/progress/:id/:token
        # PUT /api/progress/:id/:token
        #
        # 不缓存
        # ========================================================

        location /api/progress {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_no_cache 1;
            proxy_cache_bypass 1;

            proxy_read_timeout 60s;
        }


        # ========================================================
        # Recently Read
        #
        # GET /api/recently-read
        #
        # 不缓存
        # ========================================================

        location = /api/recently-read {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_no_cache 1;
            proxy_cache_bypass 1;

            proxy_read_timeout 60s;
        }


        # ========================================================
        # Reading Progress Cleanup
        #
        # POST /api/reading-progress/cleanup
        #
        # 不缓存
        # ========================================================

        location = /api/reading-progress/cleanup {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_no_cache 1;
            proxy_cache_bypass 1;

            proxy_read_timeout 60s;
        }


        # ========================================================
        # Bookshelf Status
        #
        # GET /api/bookshelf/:id/:token/status
        #
        # 不缓存
        # ========================================================

        # 已被 location /api/bookshelf 覆盖


        # ========================================================
        # Other API
        #
        # 默认不缓存
        #
        # 防止以后新增 API 时意外被缓存
        # ========================================================

        location /api/ {
            proxy_pass http://${ANGIE_BACKEND_URL};

            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;

            proxy_no_cache 1;
            proxy_cache_bypass 1;

            proxy_buffering off;

            proxy_read_timeout 60s;
            proxy_send_timeout 60s;
        }


        # ========================================================
        # EhTagTranslation
        # ========================================================

        location = /db.text.js {
            expires 1d;
            add_header Cache-Control "public";
        }


        # ========================================================
        # Health Check
        # ========================================================

        location = /healthz {
            access_log off;

            default_type text/plain;

            return 200 "ok";
        }


        # ========================================================
        # SPA
        # ========================================================

        location / {
            try_files $uri $uri/ /index.html;
        }
    }
}