# Manga Reader

单用户、自托管的 ExHentai Web 漫画客户端。

- Backend：Go / Gin / Ent (ORM) / SQLite 或 PostgreSQL / 对象存储（S3·MinIO 或本地文件）
- Frontend：Bun / React / Vite / TypeScript

## 功能

- 搜索 / 首页 / 订阅 / 热门 Gallery 列表（响应式瀑布流网格、无限滚动、缩略图懒加载与下一页预取）
- Gallery 详情、页面列表、图片代理
- 书架（Bookshelf）收藏与快照
- 阅读进度（Reading Progress）、最近阅读（Recently Read）
- 阅读器（comimi 集成）：返回 / 全屏入口、触屏进度条缩略图预览、预加载页加载动画
- 阅读记录手动清理（Cleanup API）
- 离线下载任务（Prefill）：后台排队预填充图片缓存、下载管理页、流式 ZIP 下载
- 缩略图源站代理与 MinIO 缓存
- 页面图片 MinIO 缓存（可选）
- 单用户设计，无登录、无多用户

## 目录结构

```text
manga-reader/
├── backend/            # Go Backend（见 backend/API.md）
│   └── cmd/desktop/     # Wails 桌面应用（复用同一套 REST API，见「桌面应用」）
├── frontend/           # React 前端（见 frontend/README.md）
├── Manga Reader API/   # 接口集合（OpenCollection 请求定义，同 backend/API.md）
├── .forgejo/           # CI：镜像构建 / 前端测试 / 部署
├── docker-compose.yml
└── .env.example
```

### 后端目录结构

```text
backend/
├── main.go                       # Web/Docker 入口
├── cmd/
│   └── desktop/                  # Wails 桌面入口（WebView + 进程内 Gin）
├── internal/
│   ├── app/                      # 服务生命周期：配置 → 数据库 → 存储 → Gin → 关闭
│   ├── cache/                    # 缓存对象 key（images/ thumbnail/ page-sprite/）
│   ├── storage/                  # Storage 抽象：LocalStorage / S3Storage
│   ├── config/                   # 环境变量配置
│   ├── database/                 # 数据库初始化 (Ent Client)
│   ├── ent/                      # Ent ORM 定义与生成代码
│   │   ├── schema/               # 数据模型定义 (Schema as Code)
│   │   ├── bookshelf/            # Bookshelf 字段常量与查询辅助
│   │   ├── readingprogress/      # ReadingProgress 字段常量与查询辅助
│   │   ├── prefilljob/           # PrefillJob 字段常量与查询辅助
│   │   ├── gallerycache/         # GalleryCache 字段常量与查询辅助
│   │   └── migrate/              # 数据库迁移逻辑
│   ├── exhentai/                 # ExHentai API / 页面抓取（含 10 秒请求去重）
│   ├── gallerycache/             # gallery_cache 读写
│   ├── handler/                  # HTTP 处理器 (Gin)
│   ├── imageproc/                # 图片裁剪（精灵图页面缩略图）
│   ├── model/                    # API 响应模型
│   └── ttl/                      # HTTP Cache-Control / listing cursor 常量
└── API.md                        # API 文档
```

## 快速开始（Docker Compose）

前置要求：Docker + Docker Compose。

1. 配置环境变量：

   ```bash
   cp .env.example .env
   ```

   ExHentai Cookie 可以在这里填（两种方式任选其一），也可以留空、启动后在前端
   「设置」页填写：

   ```env
   # 方式一：完整 Cookie 字符串
   EHENTAI_COOKIE=ipb_member_id=xxx; ipb_pass_hash=xxx
   # 方式二：单独变量
   EHENTAI_COOKIE_IPB_MEMBER_ID=xxx
   EHENTAI_COOKIE_IPB_PASS_HASH=xxx
   ```

2. 启动全部服务（镜像由 CI 构建并推送到镜像仓库，compose 直接拉取，仓库内无 `build:` 配置）：

   ```bash
   docker compose up -d
   ```

   更新到最新镜像：

   ```bash
   docker compose pull && docker compose up -d
   ```

   | 服务 | 端口 | 说明 |
   |------|------|------|
   | frontend | 5173 | Angie (nginx) 托管前端并反代 `/api/*`，后端不对外发布端口 |
   | backend | 8080（容器内） | Gin API，仅通过 frontend 反代访问 |

3. 打开 `http://localhost:5173` 使用。

### 数据持久化

- SQLite：`backend_data:/app/data`（`/app/data/manga-reader.db`）

数据位于 Docker 命名卷中，`docker compose down` 不会丢失；如需彻底清除使用 `docker compose down -v`。

## CI 与部署

镜像构建与部署由 Forgejo Actions（`.forgejo/workflows/`）在自托管 runner 上完成：

| Workflow | 触发 | 作用 |
|----------|------|------|
| `docker-build.yml` | push 到 `main`、PR | 检测 frontend/backend 变更 → 构建并推送 `manga-reader-frontend:latest` 与 `manga-reader-backend:latest`；push 到 `main` 且有变更时在服务器上 `docker compose pull && docker compose up -d` 完成部署（PR 只构建不部署） |
| `frontend-tests.yml` | push 到 `main`、PR | 在 `oven/bun:1` 容器内跑单测（vitest）+ reader e2e（Playwright） |
| `deploy.yml` | 手动（workflow_dispatch） | 单独执行一次 pull + 重启部署 |

本地构建同名镜像（覆盖 compose 引用的镜像，便于不依赖 CI 调试）：

```bash
docker build -t git.09270721.xyz/abc/manga-reader-frontend:latest frontend
docker build -t git.09270721.xyz/abc/manga-reader-backend:latest backend
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
| `MANGA_READER_STORAGE_DRIVER` | 否 | `auto` | 图片缓存存储：`auto`（有完整 S3 配置用 s3，否则本地文件）/ `local` / `s3`，可在设置页切换 |
| `MANGA_READER_STORAGE_DIR` | 否 | `data/cache` | `local` 驱动的缓存根目录 |
| `MANGA_READER_S3_ENDPOINT` | 否 | - | S3/MinIO 地址，如 `minio:9000`、`s3.amazonaws.com` |
| `MANGA_READER_S3_REGION` | 否 | - | Region |
| `MANGA_READER_S3_BUCKET` | 否 | - | Bucket（不存在时自动创建） |
| `MANGA_READER_S3_ACCESS_KEY` | 否 | - | 访问密钥 |
| `MANGA_READER_S3_SECRET_KEY` | 否 | - | 密钥 |
| `MANGA_READER_S3_USE_SSL` | 否 | `false` | 是否启用 SSL |
| `MANGA_READER_S3_PATH_STYLE` | 否 | `auto` | 寻址方式：`auto` / `path` / `dns` |
| `MINIO_*`（旧） | 否 | - | 旧变量名，与 `MANGA_READER_S3_*` 一一对应，新名优先 |
| `LOG_LEVEL` | 否 | `warn` | `debug`/`info`/`warn`/`error`（桌面版默认 `info`），可在设置页运行时修改 |
| `ANGIE_BACKEND_URL` | 否 | `backend:8080` | frontend(Angie) 反代后端地址（独立运行镜像时改为 `host:port`） |
| `MANGA_READER_DEV_TOOLS` | 否 | `false` | 启用 `/api/dev/*` 调试接口（可在设置页运行时开关，见 `backend/API.md` Dev Tools） |

> \* Cookie 不配置也能启动：后端会打一条 warning 并照常提供服务，缺少的凭据在前端「设置」页补上即可（否则搜索 / 在线端点会失败）。
>
> 存储驱动不配置也能启动：`auto`（默认）只在 S3 配置**齐全**时才用 S3，否则落到本地文件目录；只有显式 `MANGA_READER_STORAGE_DRIVER=s3` 而配置不全时才会启动失败。
>
> Cookie、图片缓存存储、`LOG_LEVEL`、`MANGA_READER_DEV_TOOLS` 属于「运行时设置」，见下节；其余变量仍然只在启动时读取。

## 运行时设置

设置页（前端 `/settings`）管理的配置项通过 `GET`/`PUT /api/settings` 读写：

| 分区 | 内容 |
|------|------|
| ExHentai 账号 | `EHENTAI_COOKIE_IPB_*`、`EHENTAI_COOKIE_IGNEOUS`、`EHENTAI_COOKIE_SK` |
| 图片缓存存储 | `MANGA_READER_STORAGE_DRIVER`、`MANGA_READER_STORAGE_DIR`、`MANGA_READER_S3_*` |
| 通用 | `LOG_LEVEL`、`MANGA_READER_DEV_TOOLS` |

行为约定：

- **立即生效**：保存即热替换（Cookie 换 jar、存储换后端、日志改 level、dev tools 改开关），不重启进程；进行中的请求继续使用它们启动时的那一份。
- **存数据库**：改动写入 `settings` 表（key/value），重启后继续生效；数据库中的值**优先于环境变量 / `.env`**。
- **只存差异**：只有值与环境变量不同的键才落库。把某个键改回环境变量的值会删掉对应行，于是该键重新跟随 `.env` —— 没碰过的配置项始终是「读环境变量」。
- **密钥打码**：`ipb_pass_hash`、`igneous`、`sk`、`s3.secretKey` 在响应里返回 `********`；原样回传表示「保持不变」。
- **先校验后落库**：存储配置不完整、Cookie 缺 `ipb_member_id`/`ipb_pass_hash` 等校验失败返回 `400`，进程与数据库都保持原样；存储的连通性检查失败同样返回 `400`（超时 15s）。
- 未改动的变量（端口、数据库、Cookie 引导字符串等）仍然只在启动时读取，改了要重启。

详细请求/响应见 `backend/API.md` 的 Settings 一节。

## 后端开发

```bash
cd backend
go test ./...
go vet ./...
go run .
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

| API | 数据来源 | 对象存储 | 用途 |
|-----|----------|:-----:|------|
| `/api/image/page` | ExHentai | ❌ | 阅读页图片代理（live） |
| `/api/image/thumbnail` | ExHentai | ❌ | 封面缩略图代理（live） |
| `/api/image/page-thumbnail` | ExHentai | ❌ | 页面缩略图裁剪（live） |
| `/api/image-cache/page` | 存储 / ExHentai | ✅ | 阅读页图片缓存 |
| `/api/image-cache/thumbnail` | 存储 / ExHentai | ✅ | 封面缩略图缓存（前端使用） |
| `/api/image-cache/page-thumbnail` | 存储 / ExHentai | ✅ | 页面缩略图（精灵图缓存 + 现场裁剪，前端使用） |

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

## 桌面应用（Wails Desktop）

`backend/cmd/desktop` 是一个 Wails 桌面壳：它在同一个进程里启动与 Web 版**完全相同**的 Gin REST API（`internal/app`），再把现有 React 前端塞进 WebView。没有第二套接口——前端在浏览器里能做的每件事，在这里走的都是同一套 `/api/*`。

> 完整的桌面版说明（依赖、构建、目录、配置优先级、测试与排障）见 **[`backend/cmd/desktop/README.md`](backend/cmd/desktop/README.md)**。

### 特性

- **进程内 HTTP**：API 只监听 `127.0.0.1:<随机端口>`，端口写进运行时 JSON 供前端读取，不对外暴露。
- **默认本地存储**：SQLite + 本地文件缓存，不需要 PostgreSQL / MinIO。
- **配置文件**：`~/.config/manga-reader/config.env`（`KEY=VALUE` 格式），真实环境变量优先于文件；路径可用 `MANGA_READER_CONFIG_FILE` 覆盖。
- **日志**：`~/.config/manga-reader/manga-reader.log`（桌面默认 `LOG_LEVEL=info`）。
- **运行时配置**：进程启动时写 `os.TempDir()/manga-reader-desktop.json`，退出时删除（可用 `MANGA_READER_RUNTIME_FILE` 覆盖）。

| 项 | Web/Docker 默认 | 桌面默认 |
|----|------------------|----------|
| 数据库 | `sqlite` → `data/manga-reader.db` | `sqlite` → `~/.config/manga-reader/manga-reader.db` |
| 图片缓存 | `auto`（有 S3 配置即 s3，否则本地 `data/cache`） | `local` → `~/.cache/manga-reader/cache` |
| `LOG_LEVEL` | `warn` | `info` |
| `ENVIRONMENT` | `production` | `desktop` |

### 依赖

- Go 1.27+、Bun、[go-task](https://taskfile.dev)、[Wails v3 CLI](https://wails.io)：`go install github.com/wailsapp/wails/v3/cmd/wails3@latest`
- Linux 需要 GTK3 与 `webkit2gtk` 开发头。构建由 `backend/cmd/desktop/build/` 下的 Taskfile 驱动，应用元信息见 `build/config.yml`。

### 构建 / 运行

```bash
cd backend/cmd/desktop
wails3 build     # 产物 bin/manga-reader-desktop
wails3 dev       # 开发模式：Vite 热更新 + 运行时注入 API 地址
```

Android APK 见 [构建/运行 → Android](backend/cmd/desktop/README.md#androidapk--aab)：本地用 `task android:package`，或用仓库根目录的 `build-android-docker.sh`（Podman/Docker，宿主机无需 SDK/NDK）。

首次运行需要 ExHentai Cookie（与 Web 版相同）：写在 `config.env`、环境变量里，或启动后在应用内「设置」页填写。

### 前端模式

桌面使用独立的构建模式：`bun run build:desktop`（`vite build --mode desktop`）、开发用 `bun run dev:desktop`。

API 地址解析顺序：`window.__MANGA_READER_CONFIG__`（Go 注入的 `index.html`）→ `import.meta.env.VITE_API_BASE_URL` → `http://localhost:8080`。开发模式下由 Vite 插件 `manga-reader:runtime-config` 读取上面的运行时 JSON 注入，因此不需要任何反向代理；后端的 CORS 中间件对 `wails://` 来源返回 `Access-Control-Allow-Origin: *`。

## 前端开发

见 `frontend/README.md`。

开发时通过 `VITE_API_BASE_URL` 直连后端（后端已开放 CORS 允许所有来源），无需 nginx 反代。
