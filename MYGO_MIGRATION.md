# MyGo 原生桌面迁移 TODO

> 分支：`mygo` · 首个提交：`fad66b5 feat(desktop): add mygo native UI browsing and reader`
> 更新日期：2026-10-19
>
> 目标：把整个产品改为 **MyGo 原生 UI 桌面应用**（macOS / Linux / Windows），不再提供浏览器访问和 Android APK。
> 技术边界：纯 Go + `github.com/egoist/mygo/ui` 原生控件，不使用 WebView、不构建 React 前端。

---

## 图例

- `[x]` 已完成，已通过测试
- `[ ]` 未开始
- `[ ]` **进行中**：已开工但未达到验收标准
- ❓ 待拍板 / 需要产品决策

---

## 1. 里程碑总览

| 里程碑 | 状态 | 说明 |
|---|---|---|
| M0 原生 UI 骨架与 PoC | ✅ 完成 | 窗口、侧边栏、路由、中文字体、虚拟化图片网格、阅读器原型 |
| M1 只读链路接真实 API | ✅ 基本完成 | 首页/订阅/热门/搜索/书架/最近阅读/详情/阅读器 |
| M2 单机化：进程内后端 | ⬜ 未开始 | 不再依赖外部 `MANGA_READER_API` |
| M3 本地图片缓存 | ⬜ 未开始 | 用文件系统替换 MinIO |
| M4 首次启动与设置 | ⬜ 未开始 | Cookies、数据库路径、缓存上限、主题 |
| M5 阅读体验补齐 | 🚧 进行中 | 已有翻页/缩放/平移/进度；缺缩略图、预加载、双页、全屏等 |
| M6 书架与进度 | ✅ 基本完成 | 列表、加入/移出、进度读取与上报 |
| M7 离线下载 | ⬜ 未开始 | 复用现有 prefill 接口 |
| M8 标签设置与翻译 | ⬜ 未开始 | IndexedDB 逻辑迁移到 SQLite/内存 |
| M9 数据同步 | ❓ 待定 | 是否保留同步能力 |
| M10 打包与发布 | ⬜ 未开始 | 三平台构建、图标、签名、更新 |
| M11 清理 Web/Android/Wails | ❓ 待定 | 是否删除旧前端与部署文件 |

---

## 2. 已完成（截至 `fad66b5`）

### 2.1 工程与依赖

- [x] 新建 `mygo` 分支。
- [x] `backend/go.mod` 引入 `github.com/egoist/mygo v0.5.5`，Go 指令升至 `1.27.1`。
- [x] `.gitignore` 只放行 `backend/cmd/desktop-native/`，旧 Wails 构建目录继续忽略。
- [x] 本机为旧 Wails 目录加了被忽略的嵌套 `backend/cmd/desktop/go.mod`，避免 `go build ./...` 扫描到 Wails 生成文件。**该文件不在仓库中，新克隆不需要。**
- [x] `go build ./...`、`go vet ./cmd/desktop-native/...` 通过。

### 2.2 应用骨架

- [x] `cmd/desktop-native/main.go`：MyGo 窗口、最小尺寸、`StateKey: "main"` 记住窗口状态。
- [x] `app/app.go`：应用状态、`ui.Router`、侧边栏、页面切换。
- [x] 侧边栏：首页、订阅、热门、搜索、书架、最近阅读、下载管理、标签设置、设置。
- [x] 跟随系统主题；中文字体正常渲染。
- [x] 后端不可达时显示错误页和「重试」，不会空白或崩溃。
- [x] `App.update`：后台加载结果在主线程回写，视图不阻塞网络。

### 2.3 列表与详情

- [x] 首页：`GET /api/galleries`，虚拟化 `GridView`，封面/标题/分类/页数。
- [x] 订阅：`GET /api/watched`。
- [x] 热门：`GET /api/popular`。
- [x] 搜索：`GET /api/search?q=`，输入框自动聚焦，结果实时刷新。
- [x] 书架：`GET /api/bookshelf`，显示阅读进度（读到第 N 页）。
- [x] 最近阅读：`GET /api/recently-read`。
- [x] 分页：每页 25 条，列表底部「加载更多」；刷新按钮重置列表。
- [x] 画廊详情：在线 `/api/gallery/:id/:token` 失败时回退 `/api/gallery-cache/...`。
- [x] 详情字段：标题、日文标题、分类、页数、评分、上传者、发布日期、大小、标签、封面。
- [x] 加入/移出书架：`POST/DELETE /api/bookshelf/:id/:token`，状态查询 `/status`。
- [x] 书架状态未知时显示提示，不阻塞其他内容。

### 2.4 阅读器

- [x] 页面列表：`GET /api/gallery/:id/:token/pages`，NDJSON 流式解析；失败回退缓存端点。
- [x] 读取进度：`GET /api/progress/:id/:token`，从上次页码继续。
- [x] 保存进度：翻页时异步 `PUT /api/progress/:id/:token`。
- [x] 翻页：工具栏「上一页/下一页」、`←/→`、`↑/↓`、空格。
- [x] 缩放：工具栏 `−/适应/+`、`+/-` 键、`Ctrl/Cmd + 滚轮`。
- [x] 平移：拖拽、滚轮。
- [x] `0` 键重置缩放和平移。
- [x] 底部进度条、页码/缩放百分比、快捷键提示。
- [x] 图片未加载时显示占位页面，加载完成自动重绘。
- [x] 返回画廊详情，不依赖浏览器 History。

### 2.5 图片管线

- [x] `ImageStore`：按 URL 并发下载、`ui.DecodeBitmap` 后台解码、完成请求重绘。
- [x] LRU：封面 128 张，阅读页 6 张。
- [x] 下载失败 30 秒后再次请求时重试。
- [x] 与 API 客户端共享 Basic Auth。
- [x] 封面占位图（24 种渐变），避免首帧空白。
- [x] 页面占位图（按页生成，最多缓存 4 张）。

### 2.6 测试与文档

- [x] `httptest` 假后端：列表、详情、NDJSON pages、书架、进度、图片代理。
- [x] Headless `ui.Tester` 测试：首页加载、侧边栏导航、详情、阅读器翻页、搜索过滤、阅读器返回。
- [x] `go test ./cmd/desktop-native/...` 通过。
- [x] `go test -race ./cmd/desktop-native/app` 通过。
- [x] `snapshot_test.go`（`-tags snapshot`）输出首页/详情/阅读器/搜索 PNG。
- [x] `cmd/desktop-native/README.md`：运行方式、代码结构、数据流、下一步。

---

## 3. 未完成

### 3.1 P0：单机产品化（最优先）

- [ ] **进程内启动后端**
  - [ ] 新建 `internal/desktop`（或类似）包，封装 `exhentai.CreateHTTPClient`、`database.NewDB`、`handler.New`、Gin 路由。
  - [ ] 监听 `127.0.0.1:0`，把随机端口返回给 UI。
  - [ ] 关闭窗口时优雅关闭 `http.Server`、数据库、prefill 任务。
  - [ ] `MANGA_READER_API` 保留为开发/调试覆盖；未设置时自动启动内嵌后端。
  - [ ] 单实例保护：第二次启动聚焦已有窗口，而不是再起一个后端。
- [ ] **本地文件系统图片缓存**
  - [ ] 实现 `handler.ImageCache`：`Head/Get/GetStream/Put/PutWithMeta`。
  - [ ] 缓存目录默认放在用户缓存目录（XDG / `~/Library/Caches` / `%LocalAppData%`）。
  - [ ] 原子写入（临时文件 + rename），处理并发同 key 写入。
  - [ ] 内容类型与自定义元数据用 sidecar JSON 保存。
  - [ ] UI 改用 `/api/image-cache/*` 端点，去掉对 MinIO 的依赖。
  - [ ] 缓存大小上限与清理策略（LRU/按时间）。
- [ ] **配置与首次启动**
  - [ ] 配置文件位置：`os.UserConfigDir()/manga-reader/config.json`。
  - [ ] 字段：ExHentai cookies、数据库路径、缓存目录、缓存上限、主题、语言。
  - [ ] 首次启动向导：填写 cookies，校验后进入首页。
  - [ ] Cookies 安全存储：优先系统钥匙串；至少不要明文打印到日志。
  - [ ] 设置页可修改后端地址（开发用）、缓存上限、清空缓存。
  - [ ] 启动失败时在 UI 内展示可操作的错误，而不是 `log.Fatal` 退出。
- [ ] **数据库位置**
  - [ ] 默认 SQLite 放到用户数据目录，不再依赖当前工作目录的 `data/`。
  - [ ] 启动时自动建目录、执行 Ent migrate。
  - [ ] 数据库损坏/权限不足时给出明确提示。

### 3.2 P1：阅读体验

- [ ] 页面缩略图网格（详情页）
  - [ ] 解析 `/pages` 的 `thumbnail` 精灵图坐标。
  - [ ] 调用 `/api/image-cache/page-thumbnail`（或 live 端点）。
  - [ ] 虚拟化网格、当前页高亮、点击跳页。
- [ ] 阅读器预加载
  - [ ] 预取当前页 ±1/±2。
  - [ ] 翻页时取消过期预取。
- [ ] 阅读器模式
  - [ ] 适应宽度 / 适应高度 / 原始尺寸。
  - [ ] 单页 / 双页。
  - [ ] 从右到左（日漫）阅读方向。
  - [ ] 全屏。
  - [ ] 点击屏幕左右区域翻页。
  - [ ] 滚轮翻页模式。
  - [ ] 页码跳转输入框。
- [ ] 图片加载状态
  - [ ] 加载中 spinner、失败占位与「重试」。
  - [ ] 加载失败时自动尝试 live 端点（当前只用 live）。
- [ ] 阅读器占位图优化
  - [ ] 当前占位页面在主线程同步生成，首帧可能卡顿；改为异步或纯 spinner。
- [ ] 列表体验
  - [ ] 无限滚动（当前是「加载更多」按钮）。
  - [ ] 滚动位置在返回时恢复（`GridState` 已有状态，需验证）。
  - [ ] 搜索输入防抖（当前每次按键都可能发请求）。
  - [ ] 空状态、网络错误、部分失败的统一样式。
- [ ] 书架/进度
  - [ ] 详情页显示「继续阅读第 N 页」。
  - [ ] 完成状态（最后一页）标记。
  - [ ] 阅读进度上报去抖/批量（快速翻页时当前每页一个请求）。

### 3.3 P2：现有功能迁移

- [ ] 离线下载（prefill）
  - [ ] 任务列表、进度、取消、删除、清理。
  - [ ] 单画廊 ZIP 下载/导出。
  - [ ] 下载完成后的本地阅读。
- [ ] 标签设置与翻译
  - [ ] 标签喜好（喜欢/不喜欢/屏蔽）读写。
  - [ ] 标签翻译库导入/导出。
  - [ ] IndexedDB 逻辑迁移到 SQLite 或内存缓存。
  - [ ] 标签在列表/详情中的颜色与翻译显示。
- [ ] 页面缩略图与 torrents
  - [ ] 详情页 torrents 列表与下载链接。
- [ ] 数据同步 ❓
  - [ ] 若保留：内嵌 sync host + 可选 headless server。
  - [ ] 若不保留：从设置/后端清理同步入口。
- [ ] 设置项迁移
  - [ ] 现有 `SettingsPage` 的环境变量配置改为配置文件。
  - [ ] 主题、语言、缓存、同步、代理等。
- [ ] 标签、分类、按钮文案 i18n（当前中文硬编码）。

### 3.4 P3：产品化与发布

- [ ] 三平台构建
  - [ ] macOS 12+（arm64/amd64），`.app` + 签名/公证。
  - [ ] Linux GTK3，`.deb`/AppImage 或 tarball。
  - [ ] Windows 10+，`.exe` + 安装包。
- [ ] 应用图标、启动画面、关于页。
- [ ] 自动更新（可选）。
- [ ] 崩溃日志与用户可导出的诊断信息。
- [ ] CI
  - [ ] Linux 安装 GTK3 依赖并跑 `go test`。
  - [ ] 三平台构建矩阵。
  - [ ] `go vet`、`-race`。
- [ ] 性能基准
  - [ ] 1000+ 页画廊滚动帧率。
  - [ ] 常驻内存目标 < 300MB。
  - [ ] 冷启动 < 1s（不含后端首次抓取）。
- [ ] 可访问性
  - [ ] 侧边栏/按钮/输入框的 Label、Tab 顺序、键盘操作。
- [ ] 错误上报与 toast 统一。

### 3.5 清理旧实现 ❓

- [ ] 决定是否删除/归档 `frontend/`（React）。
- [ ] 决定是否删除 Wails 桌面端（本机 `backend/cmd/desktop`）。
- [ ] 决定是否删除 Android APK、`build/android`、`build/`。
- [ ] 决定是否删除 Docker/Compose、`deploy/`、MinIO、Basic Auth、CORS、Prometheus。
  - 注意：若保留 headless server，Prometheus/Basic Auth 可能仍有价值。
- [ ] 更新根 `README.md`，把「Web + 桌面 + Android」改为「桌面单机」。

---

## 4. 待拍板决策

| # | 问题 | 选项 | 当前倾向 |
|---|---|---|---|
| 1 | 是否保留数据同步？ | 保留（可选 headless server）/ 去掉 | 需要产品确认 |
| 2 | 阅读器是否允许 `ui.WebView` 兜底？ | 纯原生 / 允许兜底 | 纯原生（符合“原生 UI”要求） |
| 3 | 传输层 | 进程内 Gin localhost / 直接函数调用 | 进程内 Gin localhost（复用现有 handler，风险最低） |
| 4 | Cookies 存储 | 系统钥匙串 / 明文配置文件 | 优先钥匙串 |
| 5 | 旧 Web/Android 代码 | 保留一段时间 / 立即删除 | 先保留，M10 后再清理 |
| 6 | 标签翻译数据 | 继续用 IndexedDB / 迁移 SQLite | 迁移 SQLite |
| 7 | 默认阅读方向 | 从左到右 / 从右到左 | 跟随画廊/用户设置 |

---

## 5. 已知问题与技术债

- [ ] 当前必须另起后端；`MANGA_READER_API` 是硬依赖。
- [ ] 图片走 live 端点，没有磁盘缓存；重复打开画廊会重复请求上游。
- [ ] 阅读页占位图在主线程生成，首帧可能有轻微卡顿。
- [ ] `ImageStore` 失败后 30 秒才重试，界面上没有可见错误。
- [ ] 搜索无防抖，快速输入会发出多个请求（旧的会被 `loadID` 丢弃）。
- [ ] 阅读进度每翻一页发一次 PUT，快速翻页会产生较多请求。
- [ ] 列表分页只有「加载更多」，没有滚动触底自动加载。
- [ ] 详情页页面缩略图仍是占位文案。
- [ ] 标签直接显示 `namespace:name`，没有颜色/翻译。
- [ ] 设置页是占位：深色模式开关、缓存信息都未接真实逻辑。
- [ ] 下载管理/标签设置是空状态页。
- [ ] 错误文案、加载文案全部硬编码中文。
- [ ] 本机 `backend/cmd/desktop/go.mod` 是本地补丁，不在仓库；若删除旧 Wails 目录可移除。
- [ ] Go 模块代理在本环境对 mygo 返回过 502；需要 `GOPROXY=https://proxy.golang.org,direct`。
- [ ] `snapshot_test.go` 需要 `-tags snapshot`，默认测试不会写文件。

---

## 6. v1 验收标准

- [ ] 单个可执行文件启动，无外部后端、无 WebView、无 Node/React 构建。
- [ ] 首次启动可填写 ExHentai cookies，校验通过后进入首页。
- [ ] 首页/搜索/详情/阅读器/书架/最近阅读全部可用。
- [ ] 阅读器支持翻页、缩放、平移、进度保存、页面缩略图。
- [ ] 断网时可读已缓存画廊；缓存有上限并可清理。
- [ ] 标签设置、下载、同步（若保留）行为与现有产品一致或有明确替代。
- [ ] macOS / Linux / Windows 均可构建运行。
- [ ] `go test ./...`、`go test -race ./cmd/desktop-native/...`、`go vet ./...` 通过。
- [ ] 1000 页画廊滚动 60fps，常驻内存 < 300MB。

---

## 7. 继续开发指南

1. 先看 `backend/cmd/desktop-native/README.md`，了解代码结构与运行方式。
2. 跑测试：
   ```bash
   cd backend
   go test ./cmd/desktop-native/...
   go test -race ./cmd/desktop-native/...
   go test -tags snapshot ./cmd/desktop-native/app
   ```
3. 按 **3.1 P0** 的顺序推进：内嵌后端 → 本地缓存 → 配置/首次启动。
4. 每完成一项，把本文件对应复选框改为 `[x]`，并在提交信息里引用里程碑（例如 `feat(desktop): embed API server (M2)`）。
5. 涉及删除旧代码（3.5）前，先在 issue/对话里确认，不要直接删。
