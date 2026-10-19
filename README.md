# Manga Reader

ExHentai 漫画客户端，原生桌面应用。

用 [mygo](https://github.com/egoist/mygo) 的 `ui` 包写的**纯 Go 原生桌面端**：无 WebView、无 React、无前端构建步骤。后端 API 在应用进程内启动，数据库与图片缓存在本地。

- 运行时：Go 1.27+
- UI：mygo 原生控件（macOS / Linux / Windows）
- 数据：SQLite + 本地文件系统图片缓存

迁移进度与剩余任务见 [`MYGO_MIGRATION.md`](MYGO_MIGRATION.md)，运行细节见 [`backend/cmd/desktop-native/README.md`](backend/cmd/desktop-native/README.md)。

## 运行

```bash
cd backend
go run ./cmd/desktop-native
```

首次启动会显示设置向导，粘贴 ExHentai 的 Cookie 请求头即可。应用会把 Cookie 写入配置目录（权限 0600），然后在 `127.0.0.1` 的随机端口启动内嵌后端，并把 SQLite 与图片缓存放到用户配置/缓存目录。

## 功能

- 首页 / 订阅 / 热门 / 搜索 / 书架 / 最近阅读，虚拟化网格 + 触底自动加载
- 画廊详情：元数据、标签、封面、页面缩略图网格、种子列表
- 阅读器：翻页、缩放、平移、进度保存、预加载、全屏、双页、左右阅读方向、四种适应模式
- 图片缓存：本地文件系统缓存，容量上限与 LRU 淘汰，断网可读已缓存内容
- 离线下载（Prefill）：任务管理、进度、取消、删除、离线阅读、ZIP 导出
- 标签设置：喜欢/不喜欢与权重、着色排序、中文翻译、源站同步

## 目录结构

```text
manga-reader/
├── backend/
│   ├── cmd/desktop-native/   # 桌面应用（mygo 原生 UI）
│   ├── internal/             # 后端：exhentai 客户端、handler、缓存、数据库
│   └── go.mod
├── Manga Reader API/         # 接口集合（OpenCollection 请求定义，同 backend/API.md）
├── .github/ .forgejo/        # CI：原生桌面端构建与测试
└── MYGO_MIGRATION.md
```

## 环境变量

桌面端通常不需要设置任何变量（用 `config.json` 与设置页即可）。以下是可用的覆盖项：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `EHENTAI_COOKIE` | - | ExHentai 完整 Cookie 字符串（跳过首次设置向导） |
| `EHENTAI_COOKIE_IPB_MEMBER_ID` / `EHENTAI_COOKIE_IPB_PASS_HASH` | - | 分开设置 Cookie 的另一种方式 |
| `EHENTAI_COOKIE_IGNEOUS` / `EHENTAI_COOKIE_SK` | - | 可选 Cookie 片段 |
| `MANGA_READER_API` | - | 指向已运行的外部后端（开发调试用），不启动内嵌后端 |
| `MANGA_READER_DB_PATH` | 用户配置目录 | SQLite 路径 |
| `MANGA_READER_CACHE_DIR` | 用户缓存目录 | 图片缓存目录 |
| `MANGA_READER_CACHE_MAX_BYTES` | `2 GiB` | 图片缓存上限，`0` 为不限 |
| `MANGA_READER_IMAGE_CACHE` | 内嵌模式下 `true` | 是否走本地图片缓存代理 |
| `MANGA_READER_APPEARANCE` | 跟随系统 | `system` / `light` / `dark` |
| `MANGA_READER_DEV_TOOLS` | `false` | 启用 `/api/dev/*` 调试接口 |
| `MANGA_READER_SYNC_TOKEN` | - | 启用服务端同步端点 |
| `GOMEMLIMIT` | - | Go 堆软上限 |

## 开发

```bash
cd backend
go test ./...                                   # 单元与端到端测试
go test ./cmd/desktop-native/...                # 桌面端（headless UI 测试）
go test -race ./internal/cache/... ./cmd/desktop-native/...
go test -tags snapshot ./cmd/desktop-native/app  # 渲染页面快照到 /tmp/opencode
```

Ent ORM 代码生成：

```bash
cd backend && go generate ./internal/ent
```

### 可观测性

`internal/metrics` 仍被后端的缓存与上游客户端使用（Prometheus 客户端库），用于内部计数；应用不再暴露 `/metrics` HTTP 端点。
