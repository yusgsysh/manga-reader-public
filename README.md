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
- 多租户：Shared Database + Shared Schema + `tenant_id` 隔离；平台管理员 / 租户管理员 / 普通用户三级角色
- 默认匿名访问（默认租户），可选 JWT 登录页（内置管理员、Argon2id 密码哈希、HttpOnly Cookie、服务端登出撤销）
- 节点同步使用数据库管理的独立 Node API Key（SHA-256 哈希、Scopes、轮换/撤销），与用户 JWT 完全分离

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
├── deploy/observability/ # 自部署监控（Prometheus + Grafana，见「自部署监控」）
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
│   ├── auth/                     # JWT 登录（Argon2id / HS256 / Gin 中间件）
│   ├── tenant/                   # 租户上下文与租户中间件
│   ├── node/                     # Node API Key 认证与 Scopes
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

   `.env.example` 默认匿名访问（默认租户）。如果要把服务暴露到公网，
   必须设置 `JWT_SECRET` 开启登录认证（见「公网部署」）。

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

## 公网部署（HTTPS + 三套独立认证）

认证按接口用途拆分，凭证互不通用：

| 接口类型 | 认证机制 |
|----------|----------|
| 用户业务 API（登录、书架、进度、Prefill、租户/用户管理） | `Authorization: Bearer <JWT>` |
| 节点同步 API（`/api/sync/push`、`/api/sync/events`） | `Authorization: Bearer <Node API Key>` |
| Metrics 内部接口 | 回环限制或独立 `MANGA_READER_METRICS_TOKEN` |
| Health / Readiness | 最小信息公开：`/healthz`、`/readyz` |

- **HTTPS 必须在认证之前**：JWT 的 HttpOnly Cookie 和节点 API Key 都必须走 TLS。TLS 由前置层（云负载均衡 / Caddy / 前置 nginx）提供，同时负责 HTTP :80 → HTTPS :443 跳转；HSTS 也由该层配置。
- **应用端口不暴露公网**：容器 `:8080`、PostgreSQL `:5432`、MinIO `:9000/:9001` 不发布到公网；compose 与 quadlet 默认发布到 `127.0.0.1:5173:8080`，公网只经过带 TLS 的前置反代。确实需要局域网直连时才改成 `0.0.0.0:5173:8080`。
- **Basic Auth 已完全移除**：后端、前端、compose/quadlet、observability 和文档都不再接受或配置 HTTP Basic Authentication。

### JWT 登录认证与多租户

多租户场景。设置随机 `JWT_SECRET` 即启用（至少 32 字节）：

```bash
openssl rand -hex 32
```

```env
JWT_SECRET=<粘贴上一步输出>
JWT_ISSUER=manga-reader
JWT_EXPIRES_IN=1h
ADMIN_USERNAME=admin
ADMIN_PASSWORD=change-me-strong-password   # 仅首次启动用于创建平台管理员
MANGA_READER_AUTH_COOKIE_SECURE=auto
```

- **默认租户与平台管理员**：首次启动创建 `default` 租户（ID 1），并用 `ADMIN_USERNAME` + `ADMIN_PASSWORD` 创建该租户的**平台管理员**；密码以 Argon2id PHC 哈希写入数据库。之后启动**不会覆盖**已有密码，可清空 `ADMIN_PASSWORD`。
- **忘记管理员密码**：不要通过改 `.env` 的 `ADMIN_PASSWORD` 来重置。新镜像内置 `reset-admin` 命令（密码从 stdin 读取，不出现在进程列表）：

  ```bash
  printf '%s\n' 'new-strong-password' | \
    docker compose exec -T backend /app/reset-admin -tenant default -username admin
  ```

  从源码运行：

  ```bash
  cd backend
  printf '%s\n' 'new-strong-password' | go run ./cmd/reset-admin -tenant default -username admin
  ```

  命令会递增 `token_version` 撤销旧 Token，并把用户状态恢复为 `active`。
- **登录接口**：`POST /api/auth/login` 需要 `tenant`（租户 slug，可省略并回退到 `default`）+ `username` + `password`。成功后返回 `access_token` / `token_type` / `expires_in`，同时写入 `HttpOnly; SameSite=Lax; Secure(auto)` Cookie；JWT 中含有 `sub`、`tenant_id`、`role`、`iss`、`iat`、`exp`、`jti`。前端不把 Token 放进 `localStorage`（降低 XSS 窃取风险），页面刷新后由 Cookie + `GET /api/auth/me` 恢复登录态。
- **角色**：`platform_admin` 管理全局租户/节点；`tenant_admin` 管理本租户用户和业务数据；`user` 只使用业务功能。角色校验同时在前端路由与后端 handler 执行。
- **数据隔离**：`bookshelf`、`reading_progress`、`prefill_job`、`tag_preference`、`tag_sync_baseline` 全部带 `tenant_id`；它们按租户共享，所有查询/创建/更新/删除都从 JWT 租户上下文取 ID。`gallery_cache` 是全局共享的上游元数据缓存。`tenant_id` 请求参数/Header 一律被忽略。
- **租户中间件**：JWT 验证后加载租户并检查 `status=active`；不存在/禁用返回 403，删除或禁用用户的 Token 会在下一次请求失效。平台管理员与租户管理员也不能跨租户访问用户或业务数据。
- **数据迁移**：旧单用户数据库启动时自动创建 `default` 租户，把旧管理员提升为平台管理员、把所有租户业务行归入租户 1；`gallery_cache` 保留。迁移在事务中执行且可重复运行。
- **CSRF**：Cookie 仅用于同源请求；`csrfGuard` 会拒绝跨站 `Origin` / `Sec-Fetch-Site` 的写请求。反向代理必须透传 `Origin` / `Sec-Fetch-Site`，并保持应用与前端同源；HTTPS 终止在反代时还要设置 `X-Forwarded-Proto: https`，否则 `auto` 模式不会给 Cookie 加 `Secure`。
- **登出**：`POST /api/auth/logout` 会递增 `users.token_version` 并清除 Cookie，所有已签发 Token 立即失效。
- **Token 校验**：只接受 `Authorization: Bearer <JWT>`（或同源 Cookie），严格校验 HS256 签名、`iss`、`iat`、`exp` 和 `token_version`；缺失/无效/过期/签名错误统一返回 `401`。
- **公开 API**：`/api/auth/login`、`/api/auth/status`、搜索/画廊/图片代理。**其余 `/api/*` 默认要求 JWT**（书架、阅读进度、最近阅读、标签设置、Prefill、同步配置、Dev Tools、`/api/admin/*`、`/api/tenant/*` 等）。
- **管理 API**：`/api/admin/tenants*` 与 `/api/admin/nodes*` 仅平台管理员；`/api/tenant/users*` 仅租户管理员，且路径中的用户 ID 必须同时匹配 JWT 租户。

### 节点同步与 API Key

- **节点**：`/api/admin/nodes` 由平台管理员管理，每个节点绑定一个租户；节点状态支持启用/禁用。
- **API Key**：按节点独立生成（至少 256 bit 随机熵），数据库只保存 SHA-256 哈希和显示前缀；完整明文只在创建/轮换响应中出现一次。
- **Scopes**：`sync:read`、`sync:write`、`sync:events`。`POST /api/sync/push` 需要 `sync:write`；`GET /api/sync/events` 需要 `sync:events`；越权返回 403。
- **轮换**：新 Key 创建后旧 Key 默认保持有效，确认后可显式 DELETE 撤销；撤销、禁用节点或禁用租户会立即拒绝认证。
- **隔离**：节点 API Key 只能访问其绑定租户；请求中的 `node_id`/`tenant_id` 不能扩大范围。同步请求记录 `request_id` 审计日志，数据合并本身按行 LWW 幂等。
- **客户端配置**：桌面端设置页填写服务器地址 + 该节点的 API Key；请求使用 `Authorization: Bearer <API Key>`。用户 JWT 不能访问同步端点。

### Metrics 与健康检查

- `/metrics` 默认启用；**未配置 Token 时仅允许回环地址访问**。需要跨主机抓取时设置独立令牌：

  ```env
  MANGA_READER_METRICS_ENABLED=true
  MANGA_READER_METRICS_TOKEN=<openssl rand -hex 32>
  ```

  请求头使用 `Authorization: Bearer <METRICS_TOKEN>`。该 Token 与 JWT_SECRET、节点 API Key 完全独立。

- `/healthz` 是存活性检查，只返回 `{"status":"ok"}`；`/readyz` 检查数据库连接，只返回 `{"status":"ready"}` 或 `503 {"status":"unavailable"}`，不暴露 DSN、路径或密钥。

### 验证

```bash
cd backend
go test ./internal/auth/ ./internal/node/ ./internal/metrics/ ./internal/sync/ ./internal/web/ ./internal/config/
cd ../frontend && bun run test
```

覆盖：JWT 登录/失效/篡改、Basic Auth 头被拒绝、节点 API Key 认证/撤销/Scopes/禁用、同步租户隔离、Metrics 回环与独立 Token、跨租户书架/进度/Prefill 隔离、角色权限与数据迁移。

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

## 自部署监控（Prometheus + Grafana）

独立 compose 项目，位于 `deploy/observability/`（与主应用分离，可部署在任意能访问 backend `/metrics` 的机器上）。Prometheus 每 15s 抓取一次指标、本地保留 15 天；Grafana 自动装载数据源与开箱即用的 dashboard。

```bash
cd deploy/observability
cp .env.example .env        # 改抓取目标、/metrics 凭据、Grafana 口令
docker compose up -d
```

| 组件 | 地址 | 说明 |
|------|------|------|
| Grafana | `http://localhost:3000` | 登录见 `.env`（默认 admin/admin，生产必改）；dashboard：**Manga Reader** |
| Prometheus | `http://localhost:9090` | 仅绑定 127.0.0.1，仅作调试 |

`.env` 关键项：

- `MANGA_READER_TARGET`：抓取目标，默认 `host.docker.internal:5173`（宿主机上主 compose 发布的 backend 端口；跨机抓取填 `http` 目标所在主机与端口）
- `MANGA_READER_METRICS_TOKEN`：与 backend 的 `MANGA_READER_METRICS_TOKEN` 一致；Prometheus 使用 `Authorization: Bearer` 抓取。未设置时 backend 的 `/metrics` 只允许回环访问。
- `GRAFANA_ADMIN_USER/PASSWORD`：Grafana 管理员

Dashboard 覆盖：HTTP QPS（按路由，过滤 `/metrics` 与 `/healthz`）/ 5xx 比例 / P95 延迟、图片与 gallery 缓存 hit rate、MinIO 操作速率与延迟、10 秒文档短共享结果（含当前条目数）、上游请求与 P95 延迟、上游在途请求与重试放大、sad_panda / IP 封禁 / HTTP 错误判定、内存预算（已用 / 上限 / 等待者 / 获取等待 P95）、goroutines / 内存 / FD。指标口径见「可观测性（Prometheus 指标）」。

告警规则在 `deploy/observability/alerting/rules.yml`（Prometheus 自动装载，`/alerts` 查看）：抓取目标失联、应用 5xx 比例持续偏高、上游出现 sad_panda / IP 封禁、FD 接近上限、goroutines / RSS 异常增长、MinIO 操作错误率、图片内存预算持续打满。

## 环境变量

全部变量见 `.env.example`：

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `EHENTAI_COOKIE` | 可选* | - | ExHentai 完整 Cookie 字符串；与单独变量同时存在时只用于补齐空缺字段 |
| `EHENTAI_COOKIE_IPB_MEMBER_ID` | 可选* | - | 登录凭据（单独设置方式，优先于完整字符串） |
| `EHENTAI_COOKIE_IPB_PASS_HASH` | 可选* | - | 登录凭据（单独设置方式，优先于完整字符串） |
| `EHENTAI_COOKIE_IGNEOUS` | 否 | - | ExHentai 会话 Cookie；过期值会被自动丢弃并重试（见「ExHentai 返回 sad panda」） |
| `EHENTAI_COOKIE_SK` | 否 | - | 标签过滤用的设置键，不参与登录 |
| `EHENTAI_PORT` | 否 | `127.0.0.1:8080` | 后端监听地址；裸跑默认仅回环，容器内由镜像/compose 覆盖为 `:8080` |
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
| `JWT_SECRET` | 否 | - | 设置后启用 JWT 登录页；至少 32 字节随机值（`openssl rand -hex 32`）。为空则保持匿名模式 |
| `JWT_ISSUER` | 否 | `manga-reader` | JWT 的 `iss`，校验时必须一致 |
| `JWT_EXPIRES_IN` | 否 | `1h` | Token 有效期（Go duration，范围 1m–720h） |
| `ADMIN_USERNAME` | 否 | `admin` | 首次初始化默认租户（`default`）的平台管理员用户名 |
| `ADMIN_PASSWORD` | 否 | - | 首次初始化平台管理员密码（至少 8 位）；仅在默认租户没有用户时使用，不覆盖已有密码 |
| `MANGA_READER_AUTH_COOKIE_SECURE` | 否 | `auto` | 认证 Cookie 的 `Secure`：`auto`（TLS 或 `X-Forwarded-Proto=https` 时开启）/`true`/`false` |
| `MANGA_READER_METRICS_ENABLED` | 否 | `true` | 启用 `/metrics`；未配置 Token 时仅回环可访问 |
| `MANGA_READER_METRICS_TOKEN` | 否 | - | 独立 Metrics Token（至少 32 字符）；与 JWT/节点 API Key 不共用 |
| `MANGA_READER_SYNC_ENABLED` | 否 | `true` | 启用节点同步宿主端点；节点与 API Key 通过数据库管理 |
| `MANGA_READER_SYNC_REQUEST_TIMEOUT` | 否 | `30s` | 单次同步 push 的 HTTP 超时（1s–5m） |
| `MANGA_READER_SYNC_API_KEY_MIN_LENGTH` | 否 | `32` | 节点 API Key 最小长度校验（不得低于 32） |
| `MANGA_READER_DEV_TOOLS` | 否 | `false` | 启用 `/api/dev/*` 调试接口（可运行时模拟 ExHentai 不可用，见 `backend/API.md` Dev Tools） |
| `MANGA_READER_MEM_BUDGET` | 否 | 不限制（`0`） | 进程内图片缓冲的字节预算（下载、精灵图解码等驻留内存的操作按大小预留，超出则排队）；默认不限制，设为正数才启用；可用 `MiB`/`MB` 后缀 |
| `HTTP_PROXY` | 否 | - | backend 出站 HTTP 代理（`http://`、`https://`、`socks5://`），仅作用于后端抓取/同步请求 |
| `HTTPS_PROXY` | 否 | - | 同上，用于 `https://` 目标（ExHentai 等） |
| `NO_PROXY` | 否 | - | 绕过代理的主机/网段（内网 MinIO、数据库、compose 服务名等） |

> \* `EHENTAI_COOKIE` 与 `EHENTAI_COOKIE_IPB_MEMBER_ID` + `EHENTAI_COOKIE_IPB_PASS_HASH` 至少配置一种，否则后端无法启动。

### ExHentai 返回 sad panda

sad panda 是 ExHentai 拒绝会话的响应（HTTP 200 + 空白页面），不是网络故障。按顺序排查：

1. `ipb_member_id` / `ipb_pass_hash` 是否还有效：改过密码或在别处退出登录都会让它们失效，需要重新从浏览器复制。
2. `igneous` 是否过期：它随每次登录轮换，旧值会让 ExHentai 直接返回 sad panda，即使 `ipb_pass_hash` 仍然有效。后端检测到 sad panda 时会丢弃该 cookie 并重试一次，成功后由响应写入新的 `igneous`，日志里会出现 `exhentai rejected the igneous cookie; retrying without it`；更新 `.env` 中的值可以省掉这次往返。
3. 出口 IP 是否被限制：ExHentai 对机房 IP、公共代理常返回 sad panda 或 IP 封禁页，可换出口或配置 `HTTPS_PROXY`。
4. `sk` 只影响标签过滤，与登录无关，缺它不会导致 sad panda。

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

后端默认监听 `127.0.0.1:8080`（仅回环；容器内为 `:8080`），完整 API 文档见 `backend/API.md`。

### 图片代理与缓存

| API | 数据来源 | MinIO | 用途 |
|-----|----------|:-----:|------|
| `/api/image/page` | ExHentai | ❌ | 阅读页图片代理（live） |
| `/api/image/thumbnail` | ExHentai | ❌ | 封面缩略图代理（live） |
| `/api/image/page-thumbnail` | ExHentai | ❌ | 页面缩略图裁剪（live） |
| `/api/image-cache/page` | MinIO / ExHentai | ✅ | 阅读页图片缓存 |
| `/api/image-cache/thumbnail` | MinIO / ExHentai | ✅ | 封面缩略图缓存（前端使用） |
| `/api/image-cache/page-thumbnail` | MinIO / ExHentai | ✅ | 页面缩略图（精灵图缓存 + 裁剪持久化，前端使用） |

缩略图缓存 Key 为 `thumbnail/<sha256(完整 URL)>`，与页面图片缓存（`images/`）相互独立。`/api/image/page-thumbnail` 与 `/api/image-cache/page-thumbnail` 支持两种寻址：`url`+`x/y/w/h`，或 `id`+`token`+`index`；精灵图缓存 Key 为 `sprite/<sha256(精灵图 URL)>`，裁剪结果持久化为 `crop/<sha256(精灵图 URL + rect)>` —— 每个格子终身只解码一次，后续请求直接命中裁剪产物（精灵图 URL 过期换新时 Key 自然失效，旧裁剪随 bucket 生命周期约 30 天过期）。冷缓存下同一精灵图不同格子的并发裁剪共享同一次解码。

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

`GET /metrics` 暴露 Prometheus 文本格式（回环限制或独立 Metrics Token；指标实现见 `backend/internal/metrics`，使用包私有 registry，测试可隔离采集）。可视化与抓取见「自部署监控（Prometheus + Grafana）」。

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `exhentai_upstream_requests_total` | Counter | `endpoint, method, status_code, outcome` | 上游请求。`outcome` 是**传输层**结果（ok = 拿到响应，**含 4xx/5xx**；error = 连接/超时失败），内容级失败见 `classification_total` |
| `exhentai_upstream_request_duration_seconds` | Histogram | `endpoint, method` | 上游延迟（send → 响应头） |
| `exhentai_upstream_response_bytes_total` | Counter | `endpoint` | 上游响应体**实际读取**字节数（透明 gzip 按解压后计；body 未被读取则不计） |
| `exhentai_upstream_retries_total` | Counter | `endpoint, reason` | 上游重试放大：`reason` = thumbnail / nl_fallback |
| `exhentai_upstream_inflight_requests` | Gauge | — | 正在进行中的上游请求数（send → 响应头） |
| `exhentai_upstream_classification_total` | Counter | `endpoint, outcome, code` | 内容级判定（sad_panda / ip_banned / http_error / error[文档解析失败]）+ 观察到的 HTTP 状态码 |
| `exhentai_fetchcache_requests_total` | Counter | `result` | 10 秒文档短共享：hit / miss / coalesced |
| `exhentai_fetchcache_entries` | Gauge | — | 短共享缓存当前条目数（写入与过期删除都会更新） |
| `image_cache_requests_total` | Counter | `kind, result` | MinIO 图片缓存查找：`kind` = page_image / thumbnail / sprite / page_crop，`result` = hit / miss |
| `gallery_cache_requests_total` | Counter | `kind, result` | `gallery_cache` 查找：`kind` = gallery / details / pages / page_thumb，`result` = hit / miss |
| `minio_cache_requests_total` | Counter | `op, result` | MinIO 操作：`op` = get / put / head，`result` = ok / not_found / error（**miss 不是 error**） |
| `minio_cache_operation_duration_seconds` | Histogram | `op` | MinIO 操作延迟 |
| `http_requests_total` | Counter | `method, route, status_code` | 本应用 HTTP 请求（含认证 401/403 与限流 429；handler panic 由 Recovery 转成 500 后同样计入） |
| `http_request_duration_seconds` | Histogram | `method, route` | 本应用 HTTP 延迟（中间件入口 → 响应完成） |
| `membudget_reserved_bytes` | Gauge | — | 当前已预留的图片负载字节数（`MANGA_READER_MEM_BUDGET` 预算内） |
| `membudget_limit_bytes` | Gauge | — | 内存预算上限；`0` 表示不限制（默认） |
| `membudget_waiters` | Gauge | — | 正阻塞等待预算释放的操作数（>0 说明预算打满、在排队） |
| `membudget_acquire_wait_seconds` | Histogram | — | 等待内存预算授予的时长 |

`http_*` 的 `route` 标签口径：

- 注册过的路由取 gin 路由模板（基数有界）；
- 静态文件与 SPA 首页由 `NoRoute` 提供，没有模板，因此显式标记为 `route="/static"` 与 `route="/spa"`；
- 其余未匹配路径（真实 404）才归入 `route="unmatched"`；
- 长连接（SSE `/api/events/changes`、`/api/sync/events`，NDJSON `/pages` 流，ZIP 下载）只计数、**不进延迟直方图**——它们能开数小时，记录会把观测全塞进 `+Inf` 桶，让该路由 P95 恒等于最后一个桶。

此外注册了 Go runtime 与进程指标（`go_goroutines`、`go_memstats_*`、`go_gc_*`、`process_resident_memory_bytes`、`process_cpu_seconds_total`、`process_open_fds` 等），无需手工采集。

Hit rate 不单独存储为 gauge，用 `rate` 计算，例如：

```promql
sum(rate(image_cache_requests_total{result="hit"}[5m]))
  /
sum(rate(image_cache_requests_total[5m]))
```

`gallery_cache_requests_total` 同理；追加 `by (kind)` 或 `kind=` 过滤可按缓存类型拆分。计数口径：每次缓存查找计一次（hit 或 miss，读取错误不计）；缓存命中的图片/缩略图以流式直出（不经缓冲读取，仍计 hit）；gallery 缓存按端点实际判定（如 details 需 `DetailsFetchedAt` 非空、pages 需 `len(Pages) > 0` 才算命中）；sprite / crop 读取在 singleflight 合并后的实际读取上各计一次。离线兜底（书架）与批量装载（最近阅读）不计入。

HTTP 5xx 率（`route` 可按接口拆分；`/metrics` 抓取与 `/healthz` 探活恒为 200，过滤掉可避免稀释）：

```promql
sum(rate(http_requests_total{status_code=~"5..",route!~"/metrics|/healthz"}[5m]))
  /
sum(rate(http_requests_total{route!~"/metrics|/healthz"}[5m]))
```

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

- **服务端**：`MANGA_READER_SYNC_ENABLED=true` 时挂载节点同步端点；节点和 API Key 由平台管理员在 `/admin/nodes` 创建，API Key 只保存 SHA-256 哈希。
- **桌面端 / 客户端**：设置页「数据同步」填服务端地址 + 该节点的一次性 API Key 并启用（配置存 `sync_state` 表）。请求使用 `Authorization: Bearer <Node API Key>`，不再使用 Basic Auth 或全局同步令牌。
- 双方通过 `POST /api/sync/push` 推送 + 拉取，`GET /api/sync/events` SSE 监听对端新变更（写后 2s 去抖推送，心跳 25s，断线退避 1s→60s，60s 轮询兜底）。节点 API Key 需要 `sync:write`（push）或 `sync:events`（SSE）。
- 合并规则：按行 LWW（`updated_at` 新者胜），删除用 tombstone；首次连接返回全量快照；更换 `server_url` 会自动重置同步游标与引导标记，对新对端重新全量（`gallery_cache` 行在 LWW 胜出时按字段合并，空值不覆盖，镜像本地 upsert 语义）。
- 节点只能访问其绑定租户的 outbox/snapshot；请求里的 `node_id`/`tenant_id` 不会扩大范围。同步请求带 `request_id` 写入审计日志。
- 桌面端本地通过 `GET /api/events/changes` SSE 通知前端刷新（该端点使用用户 JWT）。

```bash
# 平台管理员在 /admin/nodes 创建节点与 API Key 后，桌面端配置：
curl -X PUT http://localhost:8080/api/sync/config -H 'Content-Type: application/json' \
  -d '{"enabled":true,"server_url":"http://server:8080","token":"mrk_..."}'

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
