# Manga Reader

多用户、自托管的 ExHentai Web 漫画客户端。每个账号拥有独立的 Cookie、个人设置、业务数据和任务状态。

- Backend：Go / Gin / Ent (ORM) / SQLite 或 PostgreSQL / MinIO (S3)
- Frontend：Bun / React / Vite / TypeScript

## 功能

- 搜索 / 首页 / 订阅 / 热门 Gallery 列表（响应式瀑布流网格、无限滚动、缩略图懒加载与下一页预取）
- Gallery 详情、页面列表、图片代理
- 书架（Bookshelf）收藏与快照
- 阅读进度（Reading Progress）、最近阅读（Recently Read）
- 阅读器（comimi 集成）：顶栏返回 / 全屏 / 阅读设置 / 页面列表、触屏进度条缩略图预览、预加载页加载动画
- 阅读记录手动清理（Cleanup API）
- 数据同步：桌面端 ↔ 自建服务端双向实时同步阅读进度、书架与画廊缓存（SSE 推送 + LWW 合并，见 [数据同步](docs/backend-development.md#数据同步)）
- 离线下载任务（Prefill）：后台排队预填充图片缓存、下载管理页、流式 ZIP 下载
- 缩略图源站代理与 MinIO 缓存
- 页面图片 MinIO 缓存（可选）
- 多用户：实例级账号体系，JWT 中的 `user_id` 是唯一数据归属；管理员 / 普通用户两级角色
- 严格数据隔离：书架、阅读进度、标签规则、Prefill 任务、同步 outbox 全部带 `user_id`，查询/更新/删除均附加当前用户谓词
- 每用户独立 ExHentai Cookie：AES-256-GCM 加密存储、独立 Cookie Jar/HTTP Client、单独验证状态，绝不跨账号共享
- 每用户 ExHentai 设置：站点偏好与加密 Cookie 集中保存在 `user_setting`
- JWT 登录页（Argon2id 密码哈希、HttpOnly Cookie、服务端登出撤销）
- 通行密钥（WebAuthn / Passkeys）：设置页添加 / 重命名 / 删除，登录页免密码登录；凭据仅保存在用户设备，支持跨设备同步的 passkey
- TOTP 两步验证：设置页扫码绑定身份验证器，密码登录后要求 6 位验证码；提供一次性恢复码，可随时关闭或重新生成
- 同步使用数据库管理的用户级同步 Key（SHA-256 哈希、可删除/轮换，一把 Key 只同步一个账号），与用户 JWT 完全分离

## 目录结构

```text
manga-reader/
├── backend/            # Go Backend（见 backend/API.md）
├── frontend/           # React 前端（见 frontend/README.md）
├── docs/               # 部署、后端开发与问题分析文档
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
├── cmd/reset-admin/              # 管理员账号恢复 CLI
├── cmd/fetchhtml/                # 上游页面抓取调试工具
├── internal/
│   ├── auth/                     # JWT 登录、通行密钥（WebAuthn）、TOTP
│   ├── config/                   # 环境变量配置
│   ├── database/                 # 数据库初始化 (Ent Client)
│   ├── ent/                      # Ent ORM 定义与生成代码
│   │   ├── schema/               # 数据模型定义 (Schema as Code)
│   │   ├── bookshelf/            # Bookshelf 字段常量与查询辅助
│   │   ├── readingprogress/      # ReadingProgress 字段常量与查询辅助
│   │   ├── prefilljob/           # PrefillJob 字段常量与查询辅助
│   │   ├── gallerycache/         # GalleryCache 字段常量与查询辅助
│   │   └── migrate/              # 数据库迁移逻辑
│   ├── ident/                    # 16 字节 UUID 主键 / 归属类型
│   ├── model/                    # 共享数据结构（API 响应与持久化 JSON）
│   ├── exhentai/                 # ExHentai API / 页面抓取（含 10 秒请求去重）
│   ├── usercookie/               # 每用户 ExHentai Cookie 加密存储与客户端池
│   ├── synckey/                  # 用户级同步 Key 认证
│   ├── gallerycache/             # gallery_cache 表读写
│   ├── imagecache/               # MinIO 图片对象缓存（页面 / 缩略图 / 精灵图）
│   ├── storage/                  # 通用 S3 对象存储接口（ZIP 归档）
│   ├── archive/                  # ZIP 归档构建
│   ├── imageproc/                # 图片裁剪（精灵图页面缩略图）
│   ├── membudget/                # 内存预算 / 并发闸门
│   ├── metrics/                  # Prometheus 指标（私有 registry，挂在 /metrics）
│   ├── requestid/                # 请求关联 ID 中间件
│   ├── handler/                  # HTTP 处理器与路由 (Gin)
│   ├── sync/                     # 数据同步（outbox / SSE / 推送引擎，见 backend/API.md 28）
│   ├── ttl/                      # HTTP Cache-Control / listing cursor 常量
│   └── web/                      # 内嵌前端（//go:embed dist）与 JSON 响应辅助
└── API.md                        # API 文档
```

## 快速开始（Docker Compose）

前置要求：Docker + Docker Compose。

1. 配置环境变量：

   ```bash
   cp .env.example .env
   ```

   管理员账户有两种初始化方式（任选其一）：

   - **Web 端初始化（推荐）**：`ADMIN_PASSWORD` 留空，启动后打开页面会自动进入「创建管理员账户」页面，首次访问者填写用户名与密码即可完成初始化。
   - **环境变量初始化**：设置 `ADMIN_USERNAME` + `ADMIN_PASSWORD`，后端在首次启动（users 表为空）时创建管理员。`JWT_SECRET` 可留空（后端首次启动自动生成并存入数据库，重启后继续复用）：

   ```env
   JWT_SECRET=<可选，openssl rand -hex 32>
   ADMIN_USERNAME=admin
   ADMIN_PASSWORD=change-me-strong-password
   ```

   启动后登录，在「设置 → 账号」为每个账号分别填写 Cookie。
   服务端加密存储并按用户隔离；Cookie 不再通过环境变量配置。

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

## 公网部署（HTTPS + 三类凭证）

TLS、认证方式、用户级同步 Key、Metrics 与健康检查的部署要求见 [docs/deployment.md](docs/deployment.md)。

## CI 与部署

两个 CI 并存，产物都是同一个单镜像 `manga-reader`：

**Forgejo Actions**（`.forgejo/workflows/`，自托管 runner，构建 + 部署）

| Workflow | 触发 | 作用 |
|----------|------|------|
| `docker-build.yml` | push 到 `main`、PR | 检测 `frontend/`、`backend/` 或根 `Dockerfile` 变更 → 构建单镜像；push 到 `main` 时推送 `git.09270721.xyz/abc/manga-reader:latest` 与 `:<sha>`（可回滚），PR 只构建不推送、不写构建缓存；push 到 `main` 且有变更时在服务器上 `docker compose pull && docker compose up -d` 部署 compose 引用的镜像 `ghcr.io/yusgsysh/manga-reader:latest` |
| `frontend-tests.yml` | push 到 `main`、PR | 在 `oven/bun:1` 容器内跑单测（vitest）+ reader e2e（Playwright） |
| `deploy.yml` | 手动（workflow_dispatch） | 单独执行一次 pull + 重启部署 |

**GitHub Actions**（`.github/workflows/ci.yml`，托管 runner，测试 + GHCR）

| Job | 触发 | 作用 |
|-----|------|------|
| `backend-test` | push 到 `main`、PR | `gofmt` + `go vet` + `go test ./...`（`setup-go` 缓存 module/build cache） |
| `frontend-test` | push 到 `main`、PR | `bun install --frozen-lockfile`（缓存 Bun 下载目录）+ lint + 单测 + 生产构建 |
| `docker-validate` | PR | 与上面两个 job **并行**构建单镜像做校验，不推送；与发布 job 共用 `type=gha` 缓存 scope |
| `docker-publish` | push 到 `main`、手动触发 | 等两个测试 job 成功后构建并推送 `ghcr.io/yusgsysh/manga-reader`（compose / quadlet 引用的镜像） |

Dockerfile 侧的缓存策略：manifest/lockfile 先于源码复制，`bun install` 与 `go build` 使用 BuildKit cache mount（Bun 下载缓存、Go 编译缓存，不进镜像层、不占导出缓存）；Go 依赖层刻意不用 cache mount，保证 `type=gha` / `type=registry` 导出的缓存能在冷 runner 上还原 module。

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

Dashboard 覆盖：HTTP QPS（按路由，过滤 `/metrics` 与 `/healthz`）/ 5xx 比例 / P95 延迟、图片与 gallery 缓存 hit rate、MinIO 操作速率与延迟、10 秒文档短共享结果（含当前条目数）、上游请求与 P95 延迟、上游在途请求与重试放大、sad_panda / IP 封禁 / HTTP 错误判定、内存预算（已用 / 上限 / 等待者 / 获取等待 P95）、goroutines / 内存 / FD。指标口径见 [可观测性（Prometheus 指标）](docs/backend-development.md#可观测性prometheus-指标)。

告警规则在 `deploy/observability/alerting/rules.yml`（Prometheus 自动装载，`/alerts` 查看）：抓取目标失联、应用 5xx 比例持续偏高、上游出现 sad_panda / IP 封禁、FD 接近上限、goroutines / RSS 异常增长、MinIO 操作错误率、图片内存预算持续打满。

## 环境变量

全部变量见 `.env.example`：

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `EHENTAI_PORT` | 否 | `127.0.0.1:8080` | 后端监听地址；裸跑默认仅回环，容器内由镜像/compose 覆盖为 `:8080` |
| `MANGA_READER_DB_DRIVER` | 否 | `sqlite` | 数据库类型：`sqlite` 或 `postgres` |
| `MANGA_READER_DB_PATH` | 否 | `data/manga-reader.db` | SQLite 路径（Docker 内 `/app/data/manga-reader.db`） |
| `MANGA_READER_DB_DSN` | 否 | - | PostgreSQL 连接串（`postgres` 驱动时必填） |
| `DATABASE_URL` | 否 | - | 同 `MANGA_READER_DB_DSN`，作为回退 |
| `IMAGE_CACHE_ENDPOINT` | 否 | - | 图片缓存 Endpoint（AccessKey/SecretKey/Bucket 齐全即启用缓存） |
| `IMAGE_CACHE_ACCESS_KEY` | 否 | - | 图片缓存访问密钥 |
| `IMAGE_CACHE_SECRET_KEY` | 否 | - | 图片缓存密钥 |
| `IMAGE_CACHE_BUCKET` | 否 | - | 图片缓存 Bucket |
| `IMAGE_CACHE_USE_SSL` | 否 | `false` | 图片缓存是否启用 SSL |
| `IMAGE_CACHE_REGION` | 否 | - | 图片缓存 Region |
| `IMAGE_CACHE_PROVIDER` | 否 | `minio` | 图片缓存提供商（minio / s3 / 兼容 S3） |
| `ZIP_ENABLED` | 否 | `false` | 启用 ZIP 归档存储（独立于图片缓存） |
| `ZIP_PROVIDER` | 否 | `minio` | 归档存储提供商（minio / s3 / 兼容 S3，含 OSS 的 S3 兼容端点） |
| `ZIP_ENDPOINT` | 否 | - | 归档存储 Endpoint（`ZIP_ENABLED=true` 时必填） |
| `ZIP_REGION` | 否 | - | 归档存储 Region |
| `ZIP_BUCKET` | 否 | - | 归档存储 Bucket（`ZIP_ENABLED=true` 时必填） |
| `ZIP_ACCESS_KEY` | 否 | - | 归档存储访问密钥（`ZIP_ENABLED=true` 时必填） |
| `ZIP_SECRET_KEY` | 否 | - | 归档存储密钥（`ZIP_ENABLED=true` 时必填） |
| `ZIP_USE_SSL` | 否 | `false` | 归档存储是否启用 SSL |
| `ZIP_PREFIX` | 否 | `archives/` | 归档对象 Key 前缀 |
| `ZIP_CONCURRENCY` | 否 | `4` | 生成 ZIP 时并发抓取页数（1–64，顺序仍保持） |
| `ZIP_PART_SIZE` | 否 | `0` | 分片上传分片大小（字节，≥5 MiB；`0` 用 SDK 默认 16 MiB） |
| `ZIP_PRESIGN_DOWNLOAD` | 否 | `false` | 归档下载改为 302 跳转到对象存储的签名 URL（不经过后端中转；需存储端点对浏览器可达） |
| `ZIP_PRESIGN_EXPIRY` | 否 | `15m` | 签名下载 URL 有效期（1s–168h） |
| `JWT_SECRET` | 否 | - | JWT 签名与用户 Cookie 加密密钥的派生来源；留空时后端首次启动自动生成随机密钥并存入数据库（`app_setting`），重启后复用。显式设置时至少 32 字节随机值（`openssl rand -hex 32`），且优先于数据库 |
| `JWT_ISSUER` | 否 | `manga-reader` | JWT 的 `iss`，校验时必须一致 |
| `JWT_EXPIRES_IN` | 否 | `1h` | Token 有效期（Go duration，范围 1m–720h） |
| `ADMIN_USERNAME` | 否 | `admin` | 首次初始化（users 表为空）的管理员用户名（环境变量方式） |
| `ADMIN_PASSWORD` | 否 | - | 首次初始化管理员密码（至少 8 位）；仅在 users 表为空时使用，不覆盖已有密码。留空则不启动期创建管理员，改为通过 Web 端 `/setup` 页面初始化 |
| `MANGA_READER_AUTH_COOKIE_SECURE` | 否 | `auto` | 认证 Cookie 的 `Secure`：`auto`（TLS 或 `X-Forwarded-Proto=https` 时开启）/`true`/`false` |
| `MANGA_READER_METRICS_ENABLED` | 否 | `true` | 启用 `/metrics`；未配置 Token 时仅回环可访问 |
| `MANGA_READER_METRICS_TOKEN` | 否 | - | 可选独立 Metrics Token（至少 32 字符）；与 JWT、同步 Key 不共用 |
| `MANGA_READER_SYNC_ENABLED` | 否 | `true` | 启用同步宿主端点；用户级同步 Key 在「设置 → API Key」创建、数据库管理 |
| `MANGA_READER_SYNC_REQUEST_TIMEOUT` | 否 | `30s` | 单次同步 push 的 HTTP 超时（1s–5m） |
| `MANGA_READER_DEV_TOOLS` | 否 | `false` | 启用 `/api/dev/*` 调试接口（可运行时模拟 ExHentai 不可用，见 `backend/API.md` Dev Tools） |
| `MANGA_READER_MEM_BUDGET` | 否 | 不限制（`0`） | 进程内图片缓冲的字节预算（下载、精灵图解码等驻留内存的操作按大小预留，超出则排队）；默认不限制，设为正数才启用；可用 `MiB`/`MB` 后缀 |
| `HTTP_PROXY` | 否 | - | backend 出站 HTTP 代理（`http://`、`https://`、`socks5://`），仅作用于后端抓取/同步请求 |
| `HTTPS_PROXY` | 否 | - | 同上，用于 `https://` 目标（ExHentai 等） |
| `NO_PROXY` | 否 | - | 绕过代理的主机/网段（内网 MinIO、数据库、compose 服务名等） |

> ExHentai Cookie 完全按用户配置：登录后在「设置 → 账号」为每个用户单独填写，服务端加密存库。没有任何 ExHentai Cookie 相关的环境变量（`MANGA_READER_AUTH_COOKIE_SECURE` 只控制登录 JWT 的 HttpOnly Cookie 属性）。

### ExHentai 返回 sad panda

sad panda 是 ExHentai 拒绝会话的响应（HTTP 200 + 空白页面），不是网络故障。按顺序排查：

1. `ipb_member_id` / `ipb_pass_hash` 是否还有效：改过密码或在别处退出登录都会让它们失效，需要重新从浏览器复制。
2. `igneous` 是否过期：它随每次登录轮换，旧值会让 ExHentai 直接返回 sad panda，即使 `ipb_pass_hash` 仍然有效。后端检测到 sad panda 时会丢弃该 cookie 并重试一次，成功后由响应写入新的 `igneous`，日志里会出现 `exhentai rejected the igneous cookie; retrying without it`；在「设置 → 账号」重新保存 Cookie 可以省掉这次往返。
3. 出口 IP 是否被限制：ExHentai 对机房 IP、公共代理常返回 sad panda 或 IP 封禁页，可换出口或配置 `HTTPS_PROXY`。
4. `sk` 只影响标签过滤，与登录无关，缺它不会导致 sad panda。

## 后端开发

Ent 代码生成、图片代理与缓存模型、可观测性、阅读记录清理、数据同步与 Prefill 的实现细节见 [docs/backend-development.md](docs/backend-development.md)。

## 前端开发

见 `frontend/README.md`。

开发时通过 `VITE_API_BASE_URL` 直连后端（后端已开放 CORS 允许所有来源），无需 nginx 反代。
