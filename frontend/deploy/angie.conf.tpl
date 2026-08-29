# Angie (nginx 兼容) 站点配置模板
# ${ANGIE_BACKEND_URL} 由 docker-entrypoint.sh 在启动时用 envsubst 替换。
# 默认 backend:8080（compose 网络内服务名）；独立运行可传 -e ANGIE_BACKEND_URL=host:port

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

    server {
        listen 80;
        server_name _;

        root /usr/share/angie/html;
        index index.html;

        # Backend API 反代（目标由 ANGIE_BACKEND_URL 控制）
        location /api/ {
            proxy_pass http://${ANGIE_BACKEND_URL};
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_read_timeout 60s;
            proxy_send_timeout 60s;
            # cached-image / cached-thumbnail 返回二进制，禁用缓冲
            proxy_buffering off;
        }

        # SPA 路由回退
        location / {
            try_files $uri $uri/ /index.html;
        }

        # EhTagTranslation 翻译库同源加载
        location = /db.text.js {
            expires 1d;
            add_header Cache-Control "public";
        }

        # 容器健康检查
        location = /healthz {
            access_log off;
            default_type text/plain;
            return 200 "ok";
        }
    }
}
