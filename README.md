# Manga Reader

单用户、自托管的 ExHentai Web 漫画客户端。

- Backend：Go / Gin / Ent (ORM) / SQLite / MinIO (S3)
- Frontend：Bun / React / Vite / TypeScript

## 功能

- 搜索 / 首页 / 订阅 / 热门 Gallery 列表（响应式瀑布流网格、无限滚动、缩略图懒加载与下一页预取）
- Gallery 详情、页面列表、图片代理
- 书架（Bookshelf）收藏与快照
- 阅读进度（Reading Progress）、最近阅读（Recently Read）
- 阅读记录手动清理（Cleanup API）
- 离线下载任务（Prefill）：后台排队预填充图片缓存、下载管理页、流式 ZIP 下载
- 缩略图源站代理与 MinIO 缓存
- 页面图片 MinIO 缓存（可选）
- 单用户设计，无登录、无多用户

## 目录结构

```text
manga-reader/
├── backend/          # Go Backend（见 backend/API.md）
├── frontend/         # React 前端（见 frontend/README.md）
├── docker-compose.yml
└── .env.example
```

### 后端目录结构

```text
backend/
├── main.go                       # 应用入口
├── internal/
│   ├── database/                 # 数据库初始化 (Ent Client)
│   ├── ent/                      # Ent ORM 定义与生成代码
│   │   ├── schema/               # 数据模型定义 (Schema as Code)
│   │   ├── bookshelf/            # Bookshelf 查询工具
│   │   ├── readingprogress/      # ReadingProgress 查询工具
│   │   └── prefilljob/           # PrefillJob 查询工具
│   ├── handler/                  # HTTP 处理器 (Gin)
│   ├── model/                    # API 响应模型
│   ├── exhentai/                 # ExHentai API / 页面抓取
│   └── cache/                    # MinIO 对象存储缓存
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

2. 启动全部服务：

   ```bash
   docker compose up -d --build
   ```

   | 服务 | 端口 | 说明 |
   |------|------|------|
   | frontend | 5173 | Angie (nginx) 托管前端并反代 `/api/*`，后端不对外发布端口 |
   | backend | 8080（容器内） | Gin API，仅通过 frontend 反代访问 |

3. 打开 `http://localhost:5173` 使用。

### 数据持久化

- SQLite：`backend_data:/app/data`（`/app/data/manga-reader.db`）

数据位于 Docker 命名卷中，`docker compose down` 不会丢失；如需彻底清除使用 `docker compose down -v`。

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
| `MANGA_READER_DB_PATH` | 否 | `data/manga-reader.db` | SQLite 路径（Docker 内 `/app/data/manga-reader.db`） |
| `MINIO_ENDPOINT` | 否 | - | 配置后启用图片缓存 API |
| `MINIO_ACCESS_KEY` | 否 | - | MinIO 访问密钥 |
| `MINIO_SECRET_KEY` | 否 | - | MinIO 密钥 |
| `MINIO_BUCKET` | 否 | - | MinIO Bucket |
| `MINIO_USE_SSL` | 否 | `false` | 是否启用 SSL |
| `MINIO_REGION` | 否 | - | MinIO Region |

> \* `EHENTAI_COOKIE` 与 `EHENTAI_COOKIE_IPB_MEMBER_ID` + `EHENTAI_COOKIE_IPB_PASS_HASH` 至少配置一种，否则后端无法启动。

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
| `migrate/` | 数据库迁移逻辑 |

Schema 文件：
- `internal/ent/schema/bookshelf.go` — 书架模型
- `internal/ent/schema/reading_progress.go` — 阅读进度模型
- `internal/ent/schema/prefill_job.go` — 离线下载任务模型

后端默认监听 `:8080`，完整 API 文档见 `backend/API.md`。

### 图片代理与缓存

| API | 数据来源 | MinIO | 用途 |
|-----|----------|:-----:|------|
| `/api/page-image` | ExHentai | ❌ | 阅读页图片代理 |
| `/api/cached-image` | MinIO / ExHentai | ✅ | 阅读页图片缓存 |
| `/api/thumbnail` | ExHentai | ❌ | 缩略图源站代理 |
| `/api/cached-thumbnail` | MinIO / ExHentai | ✅ | 缩略图缓存（前端使用） |

缩略图缓存 Key 为 `thumbnail/<sha256(完整 URL)>`，与页面图片缓存（`images/`）相互独立。

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
  -d '{"gallery_id":123,"gallery_token":"abc","title":"My Gallery","urls":["https://exhentai.org/s/x/123-1"]}'

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
