# Manga Reader Frontend - Angie 镜像（方案 A：托管前端 + 反代 /api）
#
# 构建：
#   docker build -t manga-reader-frontend .
#
# 运行（独立，指定后端地址，如 host 上的 30080）：
#   docker run -d -p 80:80 \
#     -e ANGIE_BACKEND_URL=192.168.123.254:30080 \
#     manga-reader-frontend
#
# 或通过根目录 docker-compose.yml（frontend 服务，默认 ANGIE_BACKEND_URL=backend:8080）：
#   docker compose up -d --build frontend
#
# 说明：
# - 构建阶段用 Bun 构建生产产物；VITE_API_BASE_URL 强制为空 → 前端走同源 /api/*
# - 运行阶段用 Angie(minimal) 托管静态文件；/api 反代目标由 ANGIE_BACKEND_URL 环境变量控制

# ---- Build stage ----
FROM oven/bun:1 AS build

WORKDIR /app

# 依赖层（利用 Docker 层缓存）
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

# 源码
COPY . .
RUN bun run db:update

# 生产构建：留空 → 同源路径（与本地 .env 直连地址无关，保证镜像总是同源构建）
ENV VITE_API_BASE_URL=""
RUN bun run build

# ---- Runtime stage (Angie minimal) ----
FROM git.09270721.xyz/abc/angie:alpine

# envsubst：启动时用环境变量渲染 angie 配置
RUN apk add --no-cache gettext-envsubst

COPY deploy/angie.conf.tpl /etc/angie/angie.conf.tpl
COPY deploy/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
COPY --from=build /app/dist /usr/share/angie/html
RUN chown -R angie:angie /usr/share/angie/html \
    && chmod +x /usr/local/bin/docker-entrypoint.sh

ENV ANGIE_BACKEND_URL="backend:8080"

EXPOSE 80

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
