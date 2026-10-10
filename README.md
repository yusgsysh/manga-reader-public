# Manga Reader

单用户、自托管的 ExHentai Web 漫画客户端。

- Backend：Go / Gin / Ent (ORM) / SQLite 或 PostgreSQL / MinIO (S3)
- Frontend：Bun / React / Vite / TypeScript

## 功能

- 搜索 / 首页 / 订阅 / 热门 Gallery 列表（响应式瀑布流网格、无限滚动、缩略图懒加载与下一页预取）
- Gallery 详情、页面列表、图片代理
- 书架（Bookshelf）收藏与快照
- 阅读进度（Reading Progress）、最近阅读（Recently Read）
- 阅读器（comimi 集成）：返回 / 全屏入口、触屏进度条缩略图预览、预加载页加载动画
- 阅读记录手动清理（Cleanup API）
- 数据同步：桌面端 ↔ 自建服务端双向实时同步阅读进度、书架与画廊缓存（SSE 推送 + LWW 合并，见「数据同步」）
- 离线下载任务（Prefill）：后台排队预填充图片缓存、下载管理页、流式 ZIP 下载
- 缩略图源站代理与 MinIO 缓存
- 页面图片 MinIO 缓存（可选）
- 单用户设计，无用户系统；公网访问经内置 HTTP Basic Auth 保护（见「公网部署」）

## 目录结构

```text
manga-reader/
├── backend/            # Go Backend（见 backend/API.md）
├── frontend/           # React 前端（见 frontend/README.md）
├── Manga Reader API/   # 接口集合（OpenCollection 请求定义，同 backend/API.md）
├── Dockerfile          # 单镜像：前端构建 + 嵌入后端二进制
├── .forgejo/           # CI：镜像构建 / 前端测试 / 部署（自托管 runner）
├── .github/            # CI：测试 + GHCR 单镜像
├── deploy/quadlet/     # podman quadlet 单元
├── docker-compose.yml
└── .env.example
```

### 后端目录结构

```text
backend/
├── main.go                       # 应用入口
├── internal/
│   ├── cache/                    # MinIO 对象存储缓存
│   ├── config/                   # 环境变量配置
│   ├── database/                 # 数据库初始化 (Ent Client)
│   ├── ent/                      # Ent ORM 定义与生成代码
│   │   ├── schema/               # 数据模型定义 (Schema as Code)
│   │   ├── bookshelf/            # Bookshelf 字段常量与查询辅助
│   │   ├── readingprogress/      # ReadingProgress 字段常量与查询辅助
│   │   ├── prefilljob/           # PrefillJob 字段常量与查询辅助
│   │   ├── gallerycache/         # GalleryCache 字段常量与查询辅助
│   │   └── migrate/              # 数据库迁移逻辑
│   ├── basicauth/                # HTTP Basic Auth（apr1/bcrypt 校验 + 限流）
│   ├── exhentai/                 # ExHentai API / 页面抓取（含 10 秒请求去重）
│   ├── gallerycache/             # gallery_cache 读写
│   ├── handler/                  # HTTP 处理器 (Gin)
│   ├── imageproc/                # 图片裁剪（精灵图页面缩略图）
│   ├── metrics/                  # Prometheus 指标（私有 registry，挂在 /metrics）
│   ├── model/                    # API 响应模型
│   ├── sync/                     # 数据同步（outbox / SSE / 推送引擎，见 backend/API.md 28）
│   ├── ttl/                      # HTTP Cache-Control / listing cursor 常量
│   └── web/                      # 内嵌前端（//go:embed dist，SPA 回退与缓存策略）
└── API.md                        # API 文档
```

## 快速开始（Docker Compose）

前置要求：Docker + Docker Compose。

1. 配置环境变量：

   ```bash
   cp .env.example .env
   ```

   至少设置 ExHentai Cookie（两种方式任选其一）：

   ```env
   # 方式一：完整 Cookie 字符串
   EHENTAI_COOKIE=ipb_member_id=xxx; ipb_pass_hash=xxx
   # 方式二：单独变量
   EHENTAI_COOKIE_IPB_MEMBER_ID=xxx
   EHENTAI_COOKIE_IPB_PASS_HASH=xxx
   ```

2. 启动服务（单镜像：Gin API + 内嵌前端由 GitHub Actions 构建推送到 `ghcr.io/yusgsysh/manga-reader`，compose 直接拉取，仓库内无 `build:` 配置）。GHCR 包为私有时先登录（PAT 需 `read:packages`）：

   ```bash
   docker login ghcr.io
   docker compose up -d
   ```

   更新到最新镜像：

   ```bash
   docker compose pull && docker compose up -d
   ```

   | 服务 | 端口 | 说明 |
   |------|------|------|
   | backend | 5173 | 单镜像：静态前端（构建时 embed）与 `/api/*` 同进程由 Gin 服务 |

3. 打开 `http://localhost:5173` 使用。

### 数据持久化

- SQLite：`backend_data:/app/data`（`/app/data/manga-reader.db`）

数据位于 Docker 命名卷中，`docker compose down` 不会丢失；如需彻底清除使用 `docker compose down -v`。

## 公网部署（HTTPS + HTTP Basic Authentication）

开发环境 Basic Auth **默认关闭**（`MANGA_READER_BASIC_AUTH_ENABLED=false`），`bun dev`、`go run .`、Playwright、单元测试、桌面端均不受影响。公网部署必须显式开启，并满足：

- **HTTPS 必须在 Basic Auth 之前**：Basic Auth 只是 `base64(user:pass)`，不是加密。TLS 由前置层（云负载均衡 / Caddy / 前置 nginx）提供，同时负责 HTTP :80 → HTTPS :443 跳转；HSTS 也由该层配置，确认域名永久 HTTPS 后再开启。
- **应用端口不暴露公网**：容器 `:8080`、PostgreSQL `:5432`、MinIO `:9000/:9001` 不发布到公网，只在 Docker 内网通信；compose 的 `5173:8080` 可按需改成 `127.0.0.1:5173:8080`，公网只经过带 TLS 的前置反代。
- **保护范围**：`/`、静态资源、`/api/*`（含 SSE `/api/sync/events`、`/api/events/changes`、图片代理、Prefill、ZIP）、`/metrics`（Prometheus 指标，见「可观测性」）全部经 Basic Auth；仅 `/healthz` 豁免（`{"status":"ok"}`，无 Cookie / 令牌 / 配置信息）。认证失败返回 `401` + `WWW-Authenticate: Basic realm="Manga Reader"`，由浏览器处理——前端没有、也不需要登录页。

认证、静态资源与 API 同在一个 Gin 进程内（前端在构建时 `//go:embed` 进二进制），因此没有反代层在中间消费 `Authorization`。

### 开关与凭据

最小生产配置（`.env`，占位示例，勿提交真实口令；`secrets/` 与 `*.htpasswd` 已在 `.gitignore`）：

```env
MANGA_READER_BASIC_AUTH_ENABLED=true
MANGA_READER_BASIC_AUTH_USERNAME=admin
MANGA_READER_BASIC_AUTH_PASSWORD=change-me-strong-password
```

```bash
docker compose up -d   # 环境变量变化会重建容器
```

凭据注入三选一（启用时必须提供其一，否则后端**拒绝启动**，fail closed）：

1. **用户名 + 口令环境变量**（上例）：口令在进程内以常数时间比较校验；不写日志、不回传 API。
2. **htpasswd 文件（推荐）**：口令不进容器环境变量，经 docker secret 挂载：

   ```bash
   mkdir -p secrets
   htpasswd -nm admin > secrets/manga_reader.htpasswd   # 交互输入口令
   chmod 644 secrets/manga_reader.htpasswd              # 进程用户需可读
   ```

   ```env
   MANGA_READER_BASIC_AUTH_ENABLED=true
   MANGA_READER_BASIC_AUTH_FILE=/run/secrets/manga_reader_htpasswd
   ```

   再按 `docker-compose.yml` 末尾示例取消 `secrets:` 注释并给 backend 服务挂载该 secret。

3. **口令文件**（明文单行，docker secret）：`MANGA_READER_BASIC_AUTH_PASSWORD_FILE=/run/secrets/...` + 用户名环境变量。

注意事项：

- **哈希格式**：apr1（`$1$` / `$apr1$`，`htpasswd -m`/`-n`）与 bcrypt（`$2$`，`htpasswd -B`）均可校验；`$5$`/`$6$`（sha-crypt）不支持，会被当作验证失败（锁死而非降级）。htpasswd 文件每次请求重新读取，轮换口令无需重启。
- `.env` 中的 `$` 需写成 `$$`（compose 变量插值）。
- **限流**：Basic Auth 开启时按来源 IP 限 50 req/s（突发 200，超限 `429` + `Retry-After`），作为基础防洪水/防爆破；需要更强防爆破时在前置防火墙 / fail2ban 封禁。切勿因此放松口令强度。计数键取自 `ClientIP()`（优先 `X-Forwarded-For`）：部署在反代之后时请让前置正确传该头，否则所有请求会共享同一个限流桶。
- **桌面端同步**：同步端点同样受 Basic Auth 保护。桌面端在设置页的服务器地址内嵌凭据即可：`https://user:pass@server.example.com`（Go HTTP 客户端自动携带 `Authorization`）；本仓库部署实例即 `https://admin:你的口令@manga.09270721.xyz`（口令含 `@`、`:` 时需 URL 编码）。Basic 用户名/口令与 `MANGA_READER_SYNC_TOKEN` 是两套独立凭据，需同时正确（见 `backend/API.md` 28）。
- **安全 Header** 由 Gin 统一添加：`X-Content-Type-Options: nosniff`、`X-Frame-Options: SAMEORIGIN`、`Referrer-Policy: strict-origin-when-cross-origin`；刻意不加 CSP（避免破坏 React、SSE、图片代理）。

### 验证

```bash
cd backend
go test ./internal/basicauth/ ./internal/web/ ./internal/config/
```

覆盖：配置 fail-closed 矩阵、apr1/bcrypt 已知答案向量、`/healthz` 豁免、401 挑战头、限流 429、SPA 回退与缓存策略。

## CI 与部署

两个 CI 并存，产物都是同一个单镜像 `manga-reader`：

**Forgejo Actions**（`.forgejo/workflows/`，自托管 runner，构建 + 部署）

| Workflow | 触发 | 作用 |
|----------|------|------|
| `docker-build.yml` | push 到 `main`、PR | 检测 `frontend/`、`backend/` 或根 `Dockerfile` 变更 → 构建并推送 `git.09270721.xyz/abc/manga-reader:latest`（仅自检/备用）；push 到 `main` 且有变更时在服务器上 `docker compose pull && docker compose up -d` 部署 compose 引用的镜像 `ghcr.io/yusgsysh/manga-reader:latest`（PR 只构建不部署） |
| `frontend-tests.yml` | push 到 `main`、PR | 在 `oven/bun:1` 容器内跑单测（vitest）+ reader e2e（Playwright） |
| `deploy.yml` | 手动（workflow_dispatch） | 单独执行一次 pull + 重启部署 |

**GitHub Actions**（`.github/workflows/ci.yml`，托管 runner，测试 + GHCR）

| Job | 触发 | 作用 |
|-----|------|------|
| `backend-test` | push 到 `main`、PR | `gofmt` + `go vet` + `go test ./...` |
| `frontend-test` | push 到 `main`、PR | `bun install --frozen-lockfile` + lint + 单测 + 生产构建 |
| `docker` | 上面两个都成功后 | 构建单镜像；push 到 `main`/手动触发时推送 `ghcr.io/yusgsysh/manga-reader`（compose / quadlet 引用的镜像），PR 只构建不推送 |

本地构建同名镜像（覆盖 compose 引用的镜像，便于不依赖 CI 调试）：

```bash
docker build -t ghcr.io/yusgsysh/manga-reader:latest .
```

## 环境变量

全部变量见 `.env.example`：

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `EHENTAI_COOKIE` | 可选* | - | ExHentai 完整 Cookie 字符串 |
| `EHENTAI_COOKIE_IPB_MEMBER_ID` | 可选* | - | 单独设置方式 |
| `EHENTAI_COOKIE_IPB_PASS_HASH` | 可选* | - | 单独设置方式 |
| `EHENTAI_COOKIE_IGNEOUS` | 否 | - | 仅 ExHentai 需要 |
| `EHENTAI_COOKIE_SK` | 否 | - | 可选 |
| `EHENTAI_PORT` | 否 | `8080` | 后端监听端口 |
| `MANGA_READER_DB_DRIVER` | 否 | `sqlite` | 数据库类型：`sqlite` 或 `postgres` |
| `MANGA_READER_DB_PATH` | 否 | `data/manga-reader.db` | SQLite 路径（Docker 内 `/app/data/manga-reader.db`） |
| `MANGA_READER_DB_DSN` | 否 | - | PostgreSQL 连接串（`postgres` 驱动时必填） |
| `DATABASE_URL` | 否 | - | 同 `MANGA_READER_DB_DSN`，作为回退 |
| `MINIO_ENDPOINT` | 否 | - | 配置后启用图片缓存 API |
| `MINIO_ACCESS_KEY` | 否 | - | MinIO 访问密钥 |
| `MINIO_SECRET_KEY` | 否 | - | MinIO 密钥 |
| `MINIO_BUCKET` | 否 | - | MinIO Bucket |
| `MINIO_USE_SSL` | 否 | `false` | 是否启用 SSL |
| `MINIO_REGION` | 否 | - | MinIO Region |
| `MANGA_READER_BASIC_AUTH_ENABLED` | 否 | `false` | 内置 HTTP Basic Auth 开关；公网生产必须 `true`（见「公网部署」） |
| `MANGA_READER_BASIC_AUTH_USERNAME` | 否† | - | Basic Auth 用户名（启用且用环境变量/口令文件方式时必填） |
| `MANGA_READER_BASIC_AUTH_PASSWORD` | 否† | - | Basic Auth 口令（生产建议改用 secret 文件，勿提交真实口令） |
| `MANGA_READER_BASIC_AUTH_FILE` | 否† | - | 现成 htpasswd 文件路径（docker secret，推荐；apr1 `$1$` / bcrypt `$2$`） |
| `MANGA_READER_BASIC_AUTH_PASSWORD_FILE` | 否† | - | 单行口令文件路径（docker secret） |
| `MANGA_READER_DEV_TOOLS` | 否 | `false` | 启用 `/api/dev/*` 调试接口（可运行时模拟 ExHentai 不可用，见 `backend/API.md` Dev Tools） |
| `MANGA_READER_SYNC_TOKEN` | 否 | - | 同步宿主令牌；非空时启用服务端同步端点（`/api/sync/push`、`/api/sync/events`） |
| `HTTP_PROXY` | 否 | - | backend 出站 HTTP 代理（`http://`、`https://`、`socks5://`），仅作用于后端抓取/同步请求 |
| `HTTPS_PROXY` | 否 | - | 同上，用于 `https://` 目标（ExHentai 等） |
| `NO_PROXY` | 否 | - | 绕过代理的主机/网段（内网 MinIO、数据库、compose 服务名等） |

> \* `EHENTAI_COOKIE` 与 `EHENTAI_COOKIE_IPB_MEMBER_ID` + `EHENTAI_COOKIE_IPB_PASS_HASH` 至少配置一种，否则后端无法启动。
> † Basic Auth 启用时必须提供凭据来源之一（用户名+口令、`_FILE`、或 `MANGA_READER_BASIC_AUTH_FILE`），否则后端拒绝启动（fail closed）。

## 后端开发

```bash
cd backend
go test ./...
go vet ./...
go run .
```

`go run .` 只提供 API：静态前端通过 `//go:embed` 在编译期打包进 `internal/web/dist`，源码检出默认只有占位文件（`.gitkeep`），此时后端以 API-only 模式启动并打一条 warning。要在本地同时看到完整页面：

```bash
# 方式一：Docker 构建（CI 同款，前端由镜像构建自动嵌入）
docker build -t manga-reader .

# 方式二：本地 go run 前先放一份前端产物
cd ../frontend && bun install && bun run build
rm -rf ../backend/internal/web/dist && cp -r dist ../backend/internal/web/dist
cd ../backend && go run .
```

### Ent ORM 代码生成

后端使用 [Ent](https://entgo.io/) 作为 ORM（Schema as Code）。Schema 定义在 `internal/ent/schema/` 目录下。

修改 Schema 后需重新生成代码：

```bash
cd backend
go generate ./internal/ent/...
```

生成的代码位于 `internal/ent/` 下（由 Ent 自动生成，勿手动编辑）：

| 目录 | 说明 |
|------|------|
| `schema/` | 数据模型定义（手动编写） |
| `bookshelf/` | Bookshelf 字段常量与查询辅助 |
| `readingprogress/` | ReadingProgress 字段常量与查询辅助 |
| `prefilljob/` | PrefillJob 字段常量与查询辅助 |
| `gallerycache/` | GalleryCache 字段常量与查询辅助 |
| `migrate/` | 数据库迁移逻辑 |

Schema 文件：
- `internal/ent/schema/bookshelf.go` — 书架模型
- `internal/ent/schema/reading_progress.go` — 阅读进度模型
- `internal/ent/schema/prefill_job.go` — 离线下载任务模型
- `internal/ent/schema/gallery_cache.go` — 画廊缓存模型

后端默认监听 `:8080`，完整 API 文档见 `backend/API.md`。

### 图片代理与缓存

| API | 数据来源 | MinIO | 用途 |
|-----|----------|:-----:|------|
| `/api/image/page` | ExHentai | ❌ | 阅读页图片代理（live） |
| `/api/image/thumbnail` | ExHentai | ❌ | 封面缩略图代理（live） |
| `/api/image/page-thumbnail` | ExHentai | ❌ | 页面缩略图裁剪（live） |
| `/api/image-cache/page` | MinIO / ExHentai | ✅ | 阅读页图片缓存 |
| `/api/image-cache/thumbnail` | MinIO / ExHentai | ✅ | 封面缩略图缓存（前端使用） |
| `/api/image-cache/page-thumbnail` | MinIO / ExHentai | ✅ | 页面缩略图（精灵图缓存 + 现场裁剪，前端使用） |

缩略图缓存 Key 为 `thumbnail/<sha256(完整 URL)>`，与页面图片缓存（`images/`）相互独立。`/api/image/page-thumbnail` 与 `/api/image-cache/page-thumbnail` 支持两种寻址：`url`+`x/y/w/h`，或 `id`+`token`+`index`；精灵图缓存 Key 为 `sprite/<sha256(精灵图 URL)>`，裁剪结果不持久化，每次请求基于缓存的精灵图现场裁剪。

### 缓存模型（无 TTL + read-through）

后端缓存**没有 TTL**：`gallery_cache` 一旦写入就被永久信任，命中即返回，不再按时间回源。

- **在线端点**（`/api/gallery/*`）：**每次回源上游**，只读不写缓存 —— 保证在线时永远是最新数据。
- **上游请求短共享**：同一 `client + 方法 + URL (+表单)` 的上游页面请求（画廊 HTML、种子页、gdata 元数据）会合并并发请求，并在 **10 秒**内复用同一次抓取（`backend/internal/exhentai/fetchcache.go`），因此详情 / 页列表 / `.gpc` 总数 / 单页缩略图等多个入口不会重复抓同一个页面。这是进程内 singleflight + 短 TTL 去重，不是持久缓存。
- **缓存端点**（`/api/gallery-cache/*`）：**read-through** —— 命中直接返回（`/pages` 以与在线一致的 NDJSON 流回放），未命中则流式回源，完整成功后整份回填。
- **种子**（`/api/gallery/:id/:token/torrents{,/…}`）：后端用登录 Cookie 实时抓取并代理下载，**不缓存**。
- **图片**统一走 `/api/image-cache/*`。

缓存只在以下事件发生时更新：

| 触发 | 动作 |
|------|------|
| 缓存端点 miss | 流式回源，完整成功后整份替换 `pages` + `thumbnails` |
| 图片永久失败（旧 page URL 失效，含非 200） | 从 URL 解析 gid → 按 `gallery_id` 反查 token → 后台整份刷新（5xx 视为瞬态不触发） |
| 缓存精灵图过期（sprite 404） | 本次请求内：整份刷新 → 若 URL 未变则单页直抓绕过 10s 文档短共享 → 按 index 回填 → 重试一次 |
| 打开时 `.gpc` 页数变化 | 后台异步（SWR）比对总页数，变化则整份替换（允许缩短） |
| 单页缩略图几何缺失 / 解析超时直抓 | 按 index 稀疏补洞 |

页面 URL 与缩略图几何都支持**按 index 增量合并**（非空覆盖、空保留、不缩短）；页数变化时改为整份替换。`*_fetched_at` 仅作观测写入，不参与命中/刷新判定。

精灵图过期自愈按 `galleryID:token:index` 合并整条流程：并发请求共享一次刷新、一次直抓与一次旧/新精灵图下载，因此过期后首个请求即成功，而不是先 502 再后台刷新。

前端图片加载失败时**直接失败、不自动重试**；失败已触发后端后台刷新，用户重新打开页面即可拿到新列表（页面缩略图的精灵图过期例外，由后端在请求内自愈）。

| 层 | 项 | 值 | 说明 |
|----|----|----|------|
| 后端 | ExHentai listing cursor | 10 分钟 | 内存中的下一页游标 |
| 后端 | 上游页面/元数据请求短共享 | 10 秒 | 进程内 singleflight + TTL 去重（`fetchcache.go`） |
| HTTP | live 图片代理（`/api/image/*`） | `max-age=3600` | `Cache-Control` |
| HTTP | MinIO 内容寻址图片（`/api/image-cache/*`） | `max-age=31536000, immutable` | `Cache-Control` |
| HTTP | `/pages` 流、`/api/gallery-cache/*` | `no-store` | 不缓存 |
| MinIO | 页面图片对象 | 约 30 天 | 由 MinIO bucket 生命周期策略控制，非代码常量 |
| 前端 | react-query `staleTime` | 见 `cacheConfig.ts` | 全局 30s；pages 10m；gallery / detail / search 5m；bookshelf / recently-read / list 2m；progress / prefill 30s；settings 0 |

> 注：`backend/internal/ttl` 现在只保留 HTTP `Cache-Control` 与 listing cursor；代码中另有一批**超时/预算**常量（如 `/pages` 抓取硬上限、缩略图解析超时、上游文档超时），它们限制单次操作的耗时，并非缓存 TTL。上游页面请求的 10 秒短共享见 `backend/internal/exhentai/fetchcache.go`（进程内去重，不落库）。

### 可观测性（Prometheus 指标）

`GET /metrics` 暴露 Prometheus 文本格式（Basic Auth 之后；指标实现见 `backend/internal/metrics`，使用包私有 registry，测试可隔离采集）。

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `exhentai_upstream_requests_total` | Counter | `endpoint, method, status_code, outcome` | 上游请求（outcome = ok / error） |
| `exhentai_upstream_request_duration_seconds` | Histogram | `endpoint, method` | 上游延迟（send → 响应头） |
| `exhentai_upstream_response_bytes_total` | Counter | `endpoint` | 上游响应声明字节数（未声明则不计） |
| `exhentai_upstream_classification_total` | Counter | `endpoint, outcome` | 内容级判定（sad_panda / ip_banned / http_error） |
| `exhentai_fetchcache_requests_total` | Counter | `result` | 10 秒文档短共享：hit / miss / coalesced |
| `exhentai_fetchcache_entries` | Gauge | — | 短共享缓存当前条目数 |
| `image_cache_requests_total` | Counter | `kind, result` | MinIO 图片缓存查找：`kind` = page_image / thumbnail / sprite，`result` = hit / miss |
| `gallery_cache_requests_total` | Counter | `kind, result` | `gallery_cache` 查找：`kind` = gallery / details / pages / page_thumb，`result` = hit / miss |

Hit rate 不单独存储为 gauge，用 `rate` 计算，例如：

```promql
sum(rate(image_cache_requests_total{result="hit"}[5m]))
  /
sum(rate(image_cache_requests_total[5m]))
```

`gallery_cache_requests_total` 同理；追加 `by (kind)` 或 `kind=` 过滤可按缓存类型拆分。计数口径：每次缓存 `Get` 查找计一次（hit 或 miss，读取错误不计）；gallery 缓存按端点实际判定（如 details 需 `DetailsFetchedAt` 非空、pages 需 `len(Pages) > 0` 才算命中）；sprite 路径的外层预检与 singleflight 内复检各计一次。离线兜底（书架）与批量装载（最近阅读）不计入。

### 阅读记录清理

阅读记录不会自动清理。手动调用：

```bash
# 删除 30 天以前（默认）
curl -X POST http://localhost:8080/api/reading-progress/cleanup
# 删除 7 天以前
curl -X POST http://localhost:8080/api/reading-progress/cleanup?days=7
# 删除全部
curl -X POST http://localhost:8080/api/reading-progress/cleanup?days=0
```

### 数据同步

多实例双向同步**阅读进度、书架与画廊缓存**（settings 不同步），所有连接由桌面端主动发起（NAT 友好）：

- **服务端**：设置 `MANGA_READER_SYNC_TOKEN` 启用宿主端点，写入的数据进 outbox。
- **桌面端 / 客户端**：设置页「数据同步」填服务端地址 + 同一令牌并启用（配置存 `sync_state` 表）。服务端在 Basic Auth 之后时，地址内嵌凭据：`https://admin:你的口令@manga.09270721.xyz`。
- 双方通过 `POST /api/sync/push` 推送 + 拉取，`GET /api/sync/events` SSE 监听对端新变更（写后 2s 去抖推送，心跳 25s，断线退避 1s→60s，60s 轮询兜底）。
- 合并规则：按行 LWW（`updated_at` 新者胜），删除用 tombstone；首次连接返回全量快照；更换 `server_url` 会自动重置同步游标与引导标记，对新对端重新全量（`gallery_cache` 行在 LWW 胜出时按字段合并，空值不覆盖，镜像本地 upsert 语义）。
- 桌面端本地通过 `GET /api/events/changes` SSE 通知前端刷新（Gin 直接写出长连接，无中间代理缓冲；`WriteTimeout` 刻意未设置以支持长流）。

```bash
# 服务端
MANGA_READER_SYNC_TOKEN=my-secret go run .

# 桌面端配置
curl -X PUT http://localhost:8080/api/sync/config -H 'Content-Type: application/json' \
  -d '{"enabled":true,"server_url":"http://server:8080","token":"my-secret"}'

# 桌面端配置（公网 Basic Auth 之后：请求本身与 server_url 均带凭据）
curl -u admin:你的口令 -X PUT https://manga.09270721.xyz/api/sync/config \
  -H 'Content-Type: application/json' \
  -d '{"enabled":true,"server_url":"https://admin:你的口令@manga.09270721.xyz","token":"my-secret"}'

curl http://localhost:8080/api/sync/status
```

端点与协议详见 `backend/API.md`「28. Data Sync」。

### 离线下载（Prefill）

画廊详情页的「添加下载任务」把页面 URL 列表交给后端，由后台 worker 逐页串行抓取并写入 MinIO 缓存（约 30 天过期），不生成 ZIP、不返回图片数据。前端「下载管理」页（`/downloads`）轮询进度，支持取消、删除、批量清理，并可随时按任务流式下载 ZIP。

```bash
# 创建任务（同画廊已有排队/运行中的任务时返回该任务）
curl -X POST http://localhost:8080/api/prefill \
  -H 'Content-Type: application/json' \
  -d '{"gallery_id":123,"token":"abc","title":"My Gallery","urls":["https://exhentai.org/s/x/123-1"]}'

# 任务列表 / 单个任务（轮询进度）
curl http://localhost:8080/api/prefill
curl http://localhost:8080/api/prefill/1

# 取消 / 删除记录 / 批量清理（0 = 全部已结束记录）
curl -X POST http://localhost:8080/api/prefill/1/cancel
curl -X DELETE http://localhost:8080/api/prefill/1
curl -X POST 'http://localhost:8080/api/prefill/cleanup?days=30'

# 流式下载 ZIP（缓存缺失的页会即时补抓）
curl -OJ http://localhost:8080/api/prefill/1/zip
```

进度（`progress.done/cached/fetched`）只在任务于本进程内运行时存在，不落库；数据库仅存任务元信息与终态。进程重启后 `queued`/`running` 的任务自动重新排队。详见 `backend/API.md`。

## 前端开发

见 `frontend/README.md`。

开发时通过 `VITE_API_BASE_URL` 直连后端（后端已开放 CORS 允许所有来源），无需 nginx 反代。
