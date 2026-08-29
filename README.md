# Manga Reader

单用户、自托管的 ExHentai Web 漫画客户端。

- Backend：Go / Gin / SQLite / MinIO (S3)
- Frontend：Bun / React / Vite / TypeScript

## 功能

- 搜索 / 首页 / 订阅 / 热门 Gallery 列表
- Gallery 详情、页面列表、图片代理
- 书架（Bookshelf）收藏与快照
- 阅读进度（Reading Progress）、最近阅读（Recently Read）
- 阅读记录手动清理（Cleanup API）
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
   | frontend | 80 | Angie (nginx) 托管前端并反代 `/api/*` |
   | backend | 8080 | Gin API |
   | minio | 9000 / 9001 | 对象存储（控制台 9001） |

3. 打开 `http://localhost` 使用。

### 数据持久化

- SQLite：`backend_data:/app/data`（`/app/data/manga-reader.db`）
- MinIO：`minio_data:/data`

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

## 前端开发

见 `frontend/README.md`。

开发时通过 `VITE_API_BASE_URL` 直连后端（后端已开放 CORS 允许所有来源），无需 nginx 反代。
