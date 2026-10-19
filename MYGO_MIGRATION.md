# MyGo 原生桌面迁移 TODO

> 分支：`mygo` · 关键提交：`fad66b5`（原生浏览/阅读器）、`81e3621`（迁移 TODO）、本次「内嵌后端 + 本地文件缓存」
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
| M2 单机化：进程内后端 | ✅ 完成 | `cmd/desktop-native/internal/desktop` 启动 Gin；`MANGA_READER_API` 变为可选覆盖；单实例 + 每次启动的随机访问令牌 |
| M3 本地图片缓存 | ✅ 完成 | FS 缓存 + UI 默认走 `/api/image-cache/*` + 容量上限与按时间淘汰 |
| M4 首次启动与设置 | ✅ 基本完成 | 首次设置向导 + 设置页（路径、缓存、Cookies、主题、语言）；缺钥匙串 |
| M5 阅读体验补齐 | 🚧 进行中 | 翻页/缩放/平移/进度/预加载/全屏/缩略图/点击翻页/跳页/RTL/双页/滚轮翻页已完成；缺精确适应模式与双页 RTL 顺序 |
| M6 书架与进度 | ✅ 基本完成 | 列表、加入/移出、进度读取与上报 |
| M7 离线下载 | 🚧 进行中 | prefill 任务列表/进度/取消/删除/ZIP 导出已接，缺完成后的独立离线入口 |
| M8 标签设置与翻译 | ⬜ 未开始 | IndexedDB 逻辑迁移到 SQLite/内存 |
| M9 数据同步 | ❓ 待定 | 是否保留同步能力 |
| M10 打包与发布 | ⬜ 未开始 | 三平台构建、图标、签名、更新 |
| M11 清理 Web/Android/Wails | ❓ 待定 | 是否删除旧前端与部署文件 |

---

## 2. 已完成（截至 `fad66b5` + 本次内嵌后端提交）

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
  - 修复：页面列表请求原先**没带内嵌后端的访问令牌**（`pages()` 手写请求，绕过了 `Client.do`），内嵌模式下一律 401，阅读器永远停在「正在获取页面列表」。现与 `do` 一样携带 `X-Manga-Reader-Token`。
  - 修复：页面列表改为**边收边显示**（`PagesStreaming`）。实测 1585 页画廊后端需约 **50 秒**抓完，原实现要等整个流结束才显示；现在首批页面到达即可阅读，工具栏显示「已加载 N / M 页…」。
  - 修复：加载失败后 `ensureReader` 原本每帧都会重新发起加载（`loading`/`loaded` 都为空），形成闪烁的无限重试；新增 `started` 标记，失败后停留在错误页直到点「重试」。
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

### 2.7 内嵌后端与本地缓存（本次新增）

- [x] `internal/cache/fscache.go`：文件系统实现 `handler.ImageCache`（`Head/Get/GetStream/Put/PutWithMeta`）。
- [x] 原子写入（临时文件 + rename）；sidecar JSON 保存 content-type / cache-control / user metadata。
- [x] key 二次哈希 + 两级目录，杜绝路径穿越；写入时 sidecar 先行，读取时缺 sidecar 会 `DetectContentType` 兜底。
- [x] `cache.IsNotFound` 识别 `fs.ErrNotExist`，文件缓存 miss 与 MinIO miss 行为一致。
- [x] `internal/config` 导出 `ParseCookieString`，供桌面设置读取 cookie 串。
- [x] `cmd/desktop-native/internal/desktop`：进程内启动现有 Gin API，监听 `127.0.0.1:0`。
- [x] 复用 `exhentai` 客户端、`database.NewDB`、`handler.New`、sync service 与启动清理逻辑。
- [x] `internal/desktop/settings.go`：`config.json` 读写、环境变量覆盖、默认数据库/缓存路径。
- [x] `main.go`：未设置 `MANGA_READER_API` 时启动内嵌后端；退出时优雅关闭（`http.Server`、sync、prefill、DB）。
- [x] UI 图片默认走缓存代理：`Client.UseImageCache(true)`，`/api/image-cache/{thumbnail,page,page-thumbnail}`；内嵌模式自动开启，`MANGA_READER_IMAGE_CACHE` 可覆盖。
- [x] 缺少 cookies 时给出指向 `config.json` 的明确错误。
- [x] 新增测试：FS cache 读写 / miss / 流式 / 元数据 / 覆盖 / 路径穿越 / 清理；设置解析与优先级；内嵌服务器 `/healthz`、`/api/bookshelf`、缓存缩略图走本地文件；UI 图片 URL 走缓存端点。
- [x] **首次设置向导**：无 cookie 时整个窗口显示引导页（TextArea + 保存并启动），成功后写入 `config.json` 并启动内嵌后端、切到首页。
- [x] **设置页**：显示后端地址、配置文件、数据库、缓存目录与缓存占用（`FSCache.Size`），可设置缓存上限（MB）、清空缓存（`FSCache.Clear`）、重新配置 Cookies。
- [x] **缓存上限与淘汰**：`FSCache.SetMaxBytes` + `Prune`（按 mtime 淘汰数据与 sidecar，保留 10% 余量）；写入后超限异步清理；`MANGA_READER_CACHE_MAX_BYTES` 与设置页可配，默认 2 GiB。
- [x] `Client` 的 base URL 改为并发安全可变（`SetBaseURL`），首次设置后无需重建 app；`Client.UseImageCache` 同样加锁。
- [x] `desktop.SaveSettings`：原子写入、权限 0600。
- [x] `desktop.Server.ClearCache` / `CacheInfo` / `CacheMaxBytes` / `SetCacheMaxBytes`，`FSCache.Size` / `Prune`。
- [x] **内嵌后端访问令牌**：每次启动随机生成（`crypto/rand`），`Options.AuthToken` + Gin 中间件常量时间校验；API 客户端与 `ImageStore` 携带 `X-Manga-Reader-Token`，并支持 `access_token` 查询参数兜底；缺失/错误返回 401。
- [x] **单实例**：`internal/singleinstance` 在配置目录监听 `instance.sock`，第二个进程连接并发送 `focus` 后退出，主进程 `Window.Show()/Focus()`；崩溃遗留的 socket 会被探测并接管。
- [x] 新增测试：设置保存往返 + 0600 权限；缓存 size/clear/prune（按最旧淘汰、超限自动清理、上限开关）；缓存上限解析与 env 覆盖；首次设置连接后端、空 cookie 拒绝；设置页缓存占用/上限/清空；保存上限与非法输入校验；令牌中间件（缺失/正确/错误/查询参数）；客户端与 ImageStore 携带令牌；单实例（主/次、聚焦回调、Close 后接管、过期 socket 接管）；搜索防抖（连续输入只发一次请求）；阅读器预加载（相邻页经缓存代理预取）；详情页缩略图网格（经缓存代理加载、点击跳到指定页）；阅读器点击左右翻页、适应模式切换、页码跳转、RTL 方向键；离线下载（创建任务、轮询到已完成、删除后不再回到列表、ZIP 导出到临时目录、默认文件名清洗）；标签规则（添加/删除/保存、权重校验、着色与排序、同步结果、列表卡片着色、任意标签拆分、快照）；种子列表（按需加载、保存 .torrent、文件名清洗）；标签翻译库（JSONP/纯 JSON 解析、HTML 清洗、缓存加载、chip 翻译）；进度批量写（连续翻页只写最后一页）；双页与滚轮翻页（步进、模式切换）；图片加载失败占位与重试；列表触底自动加载下一页与加载更多失败重试；关于行（版本注入与平台）；诊断信息导出（不含敏感字段）；统一错误块、入队 toast、任务标题跳转画廊；设置页主题持久化与生效；setup/settings 快照。
- [x] 本地实跑：无 cookie 进入设置界面；有 cookie 自动启动内嵌服务并写入 `config.json`。
- [x] `go test ./...`、`go test -race ./internal/cache/... ./cmd/desktop-native/...`、`go vet`、`go build ./...` 通过。
- [x] 本地实跑：`go run ./cmd/desktop-native` 打印内嵌地址并打开窗口。

---

## 3. 未完成

### 3.1 P0：单机产品化（最优先）

- [x] **进程内启动后端**
  - [x] 新建 `internal/desktop` 包，封装 `exhentai.CreateHTTPClient`、`database.NewDB`、`handler.New`、Gin 路由。
  - [x] 监听 `127.0.0.1:0`，把随机端口返回给 UI。
  - [x] 关闭窗口时优雅关闭 `http.Server`、数据库、prefill 任务。
  - [x] `MANGA_READER_API` 保留为开发/调试覆盖；未设置时自动启动内嵌后端。
  - [x] 单实例保护：第二次启动通过 `instance.sock` 请求已有实例聚焦窗口，而不是再起一个后端。
  - [x] 内嵌后端每次启动生成随机访问令牌，UI 客户端与本机图片请求都携带；其他本机进程 401。
- [x] **本地文件系统图片缓存**
  - [x] 实现 `handler.ImageCache`：`Head/Get/GetStream/Put/PutWithMeta`。
  - [x] 缓存目录默认放在用户缓存目录（XDG / `~/Library/Caches` / `%LocalAppData%`）。
  - [x] 原子写入（临时文件 + rename），处理并发同 key 写入。
  - [x] 内容类型与自定义元数据用 sidecar JSON 保存。
  - [x] UI 改用 `/api/image-cache/*` 端点（内嵌模式默认开启，`MANGA_READER_IMAGE_CACHE` 可覆盖）。
  - [x] 缓存大小上限与清理策略：`FSCache` 支持 `SetMaxBytes`，`Put` 后超过上限会按修改时间异步淘汰最旧对象（数据与 sidecar 一起删除，保留 10% 余量）。默认 2 GiB，`MANGA_READER_CACHE_MAX_BYTES` 可覆盖，设置页可改（`0` 为不限）。
- [ ] **配置与首次启动**
  - [x] 配置文件位置：`os.UserConfigDir()/manga-reader/config.json`（写入权限 0600）。
  - [x] 字段：ExHentai cookie、数据库路径、缓存目录、同步 token、dev tools、缓存上限。
  - [ ] 字段：主题、语言、阅读方向。
  - [x] 首次启动向导：填写 cookies，校验后启动内嵌后端并进入首页。
  - [ ] Cookies 安全存储：优先系统钥匙串；目前明文存 `config.json`（0600）。
  - [x] 设置页：显示后端地址/配置文件/数据库/缓存目录/缓存占用，支持设置缓存上限、清空缓存、重新配置 Cookies。
  - [ ] 启动失败时在 UI 内展示可操作的错误（目前后端启动失败会回落到设置页并展示错误，但 `LoadSettings` 解析失败仍是 `log.Fatal`）。
- [x] **数据库位置**
  - [x] 默认 SQLite 放到用户配置目录，不再依赖当前工作目录的 `data/`。
  - [x] 启动时自动建目录、执行 Ent migrate。
  - [ ] 数据库损坏/权限不足时给出明确提示。

### 3.2 P1：阅读体验

- [x] 页面缩略图网格（详情页）
  - [x] 解析 `/pages` 的 `thumbnail` 精灵图坐标（后端按 `id`+`token`+`index` 切图）。
  - [x] 调用 `/api/image-cache/page-thumbnail`（或 live 端点）。
  - [x] 虚拟化网格（`GridView` 只构建可见格）、点击跳转到对应页并覆盖保存的进度。
- [ ] 阅读器预加载
  - [x] 预取当前页 ±1/±2（`readerPreloadPages = 2`，含前一页；当前页优先入队）。
  - [ ] 翻页时取消过期预取（当前依赖 `ImageStore` 的窗口上限，不主动取消）。
- [ ] 阅读器模式
  - [x] 适合页面（`Contain`）/ 适应宽度（`Cover`，溢出部分可拖拽查看）。
  - [x] 原始尺寸（`NaturalSize`；工具栏循环：适合页面 → 适应宽度 → 原始尺寸）。
  - [x] 适应高度：按位图宽高比与窗口高度算出显示尺寸（双页减半），工具栏循环为 适合页面 → 适应宽度 → 适应高度 → 原始尺寸。
  - [x] 单页 / 双页（双页按两页步进并保持偶数起始页；RTL 下左右顺序由图像内容决定，未反转）。
  - [x] 从右到左（日漫）阅读方向：方向键与点击区域互换。
  - [x] 全屏（工具栏按钮 + `F` 快捷键，`Window.ToggleFullScreen`）。
  - [x] 点击屏幕左右区域翻页（放大后不触发，避免与拖拽冲突）。
  - [x] 滚轮翻页模式（可切换；滚轮平移模式下到底/到顶也会翻页）。
  - [x] 页码跳转输入框（底部栏，回车/按钮跳转并保存进度）。
- [x] 图片加载状态
  - [x] 加载中 spinner、失败占位与「重试」（阅读器页面与列表封面）。
  - [ ] 加载失败时自动尝试 live 端点（当前只用 live）。
- [ ] 阅读器占位图优化
  - [x] 阅读页占位图改为后台生成（`PageCache.Get` 未就绪时返回 nil，`build` 在 goroutine 里画完再请求一帧），不再在渲染帧里同步算 900×1300 渐变。
- [x] 列表体验
  - [x] 无限滚动：`GridView` 懒构建的卡片在布局后若落在窗口底部两行内，就置 `listState.atEnd` 并加载下一页；`GridView` 未公开可见行范围，故用卡片自身 `Bounds()` 判断。
  - [x] 翻页失败时底部显示「加载更多失败」与「重试」；加载中显示 spinner。
  - [x] 滚动位置在返回时恢复：列表状态与 `GridState` 按路径保留，从详情返回既不重建网格也不重新加载（`TestListKeepsItemsWhenReturning`）。
  - [x] 搜索输入防抖（300ms；`searchSeq` 让过期定时器失效，最后一次输入才发请求）。
  - [ ] 空状态、网络错误、部分失败的统一样式。
- [x] 书架/进度
  - [x] 详情页按钮显示「继续阅读（第 N 页）」。
  - [ ] 完成状态（最后一页）标记。
  - [x] 阅读进度上报批量：翻页 600ms 后只写最后一页；返回详情页前立即补写。

### 3.3 P2：现有功能迁移

- [x] 离线下载（prefill）
  - [x] 任务列表、进度、取消、删除（`/downloads` 页；任务列表来自 `GET /api/prefill`，运行中的任务每 1.5s 轮询一次）。
  - [x] 详情页「离线下载」：先取页面列表再 `POST /api/prefill`，完成后按钮变为「查看下载」。
  - [x] 单画廊 ZIP 导出：下载页或详情页「导出 ZIP」，用原生保存对话框选择路径后流式写盘（`/api/prefill/:id/zip`）。
  - [x] 下载完成后的离线阅读入口：下载管理页对已完成任务提供「离线阅读」，打开 `/reader/:id/:token?offline=1`；离线模式直接读 `/api/gallery-cache/*`，不等上游超时，工具栏标注「离线」。
- [ ] 标签设置与翻译
  - [x] 标签喜好（喜欢/不喜欢）读写：`/tag-settings` 页支持添加（含权重 -999..999）、删除、全量保存。
  - [x] 标签在详情页的颜色（蓝/红）与排序（喜欢 → 不喜欢 → 其余，权重大者靠前）。
  - [ ] 源站同步（`POST /api/tag-settings/sync`）：已接「确认删除/冲突处理/同步」按钮与结果摘要，冲突逐条选择 UI 未做。
  - [x] 标签翻译库：解析 EhTagTranslation 的 `db.text.json`（44k+ 条），缓存到 `<cache 目录上级>/tag-translations.json`，后台加载；标签页可搜索并点选填入规则。
  - [x] 翻译显示在标签 chip 上（`中文 / namespace:name`）。
  - [x] 「更新翻译库」从 GitHub releases 拉取最新版本并原子替换缓存。
  - [ ] 标签翻译库导入/导出（自定义库）。
  - [x] 列表卡片/搜索结果也显示前两个标签并按规则着色（`Gallery.Tags` 改为结构体，保留命名空间）。
- [x] 页面缩略图与 torrents
  - [x] 详情页 torrents 列表（按需加载）与 `.torrent` 下载（原生保存对话框）。
- [ ] 数据同步 ❓
  - [ ] 若保留：内嵌 sync host + 可选 headless server。
  - [ ] 若不保留：从设置/后端清理同步入口。
- [ ] 设置项迁移
  - [x] 环境变量配置改为 `config.json`（cookie/数据库/缓存/上限/主题），env 仍可覆盖。
  - [x] 主题：跟随系统 / 浅色 / 深色，写入 `appearance` 并在启动时 `mygo.Theme.SetSource`；运行时切换需重启。
  - [x] 语言：界面语言行显示「简体中文」，日期/数字跟随系统区域。
  - [ ] 代理、同步开关等其余设置项。
- [ ] 标签、分类、按钮文案 i18n（当前中文硬编码）。

### 3.4 P3：产品化与发布

- [~] 三平台构建
  - [x] 交叉编译验证：`GOOS=windows/darwin go build ./cmd/desktop-native/...` 通过（含 `mygo` 依赖）。
  - [ ] macOS 12+（arm64/amd64），`.app` + 签名/公证。
  - [ ] Linux GTK3，`.deb`/AppImage 或 tarball。
  - [ ] Windows 10+，`.exe` + 安装包。
- [ ] 应用图标、启动画面。
- [x] 关于：设置页显示版本（`-ldflags -X main.version=…`，未注入时为 `dev`）与平台（`GOOS/GOARCH`）。
- [ ] 自动更新（可选）。
- [x] 用户可导出的诊断信息：设置页「导出诊断信息」写出文本（版本、平台、路径、缓存大小、翻译库/规则/任务数量），不含 Cookie 与图片。
- [ ] 崩溃日志（当前只有进程 stderr）。
- [x] CI
  - [x] Linux 安装 GTK3 依赖并跑 `go test` / `go vet` / `go test -race`（`.github/workflows/native-desktop.yml`，并在 `.forgejo/workflows/` 同步一份给自托管仓库）。
  - [x] 三平台矩阵：linux/amd64、darwin/arm64、darwin/amd64、windows/amd64 交叉编译 app + desktop 内部包 + 后端包；Linux 额外产出完整二进制 artifact。
  - [ ] 正式发布流水线（签名、公证、`.deb`/`.exe` 安装包）。
- [ ] 性能基准
  - [x] 1000+ 页画廊滚动帧率：`BenchmarkScrollHome` / `BenchmarkScrollThumbnails` 以虚拟化网格渲染 1000 个画廊与 1000 页缩略图，滚动帧成本不随列表长度增长。
  - [x] 常驻内存目标 < 300MB：`TestMemoryUnderTarget` 实测 1000 页缩略图网格仅占 **约 1.4MB 堆**（远低于目标；GPU 路径下纹理另计）。
  - [x] 滚动帧预算：`TestScrollingStaysUnderBudget` 实测 CPU 软件渲染器平均 **约 7.4ms/帧**（60fps 预算 16.7ms 内）；GPU 路径只会更快。
  - [x] 冷启动 < 1s（不含后端首次抓取）：`TestColdStartUnderBudget` 实测首帧 **约 120ms**，列表出现约 146ms。
  - [x] `ImageStore` 淘汰改为真正的 LRU：命中会把条目移到最近使用端（旧实现按加载顺序 FIFO 淘汰，**会淘汰用户正在看的页**）；淘汰与 `Retry` 从 O(n) 扫描改为链表 O(1)，实测淘汰开销比旧实现低约 35%、分配少约 40%。
- [ ] 可访问性
  - [ ] 侧边栏/按钮/输入框的 Label、Tab 顺序、键盘操作。
- [x] 错误与反馈统一：所有页面共用 `errorBlock`（红色「请求失败」徽章 + 后端消息 + 重试）；离线下载入队成功用 toast；下载任务标题可点击回到画廊。

### 3.5 清理旧实现 ❓

- [ ] 决定是否删除/归档 `frontend/`（React）。
- [ ] 决定是否删除 Wails 桌面端（本机 `backend/cmd/desktop`）。
- [ ] 决定是否删除 Android APK、`build/android`、`build/`。
- [ ] 决定是否删除 Docker/Compose、`deploy/`、MinIO、Basic Auth、CORS、Prometheus。
  - 注意：若保留 headless server，Prometheus/Basic Auth 可能仍有价值。
- [x] 根 `README.md` 顶部加了原生桌面版说明与运行方式（旧 Web/Android 文档保留，待 3.5 决策后清理）。

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

- [x] ~~当前必须另起后端；`MANGA_READER_API` 是硬依赖。~~ 已内嵌，未设置时本进程启动。
- [x] ~~图片走 live 端点，没有磁盘缓存。~~ 默认走 `/api/image-cache/*` 本地文件缓存。
- [x] ~~`FSCache` 没有容量上限与淘汰策略；~~ 已加容量上限与按 mtime 淘汰（写入后超限异步清理，保留 10% 余量）；`Clear()` 与上限设置均已接到设置页。
- [ ] 缓存淘汰以"按 mtime 从旧到新"为准，没有访问频次（LRU）；命中不会刷新 mtime，因此热点图片也可能被淘汰。
- [ ] `Prune` 在每次超限时都会完整遍历缓存树；对象数量很大时开销可观（有 seeded 近似值做快速路径减轻）。
- [x] ~~首次启动仍是 `log.Fatal`，缺少引导界面；~~ 已加首次设置向导（cookie 仍明文存 `config.json`，权限 0600）。
- [x] ~~内嵌后端监听 loopback 且无鉴权；~~ 已加每次启动的随机令牌中间件（常量时间比较，401 拒绝）。
- [ ] 单实例用 Unix domain socket 实现；Windows 若 `net.Listen("unix", …)` 不可用会退化为不做单实例（仅记录日志）。
- [ ] 阅读页占位图在主线程生成，首帧可能有轻微卡顿。
- [x] ~~`ImageStore` 失败后 30 秒才重试，界面上没有可见错误。~~ 已加 `Err`/`Retry`，封面与阅读页会显示失败并支持点击重试。
- [x] ~~搜索无防抖，快速输入会发出多个请求（旧的会被 `loadID` 丢弃）。~~ 已加 300ms 防抖。
- [x] ~~阅读进度每翻一页发一次 PUT。~~ 已改为 600ms 防抖 + 离开阅读器时立即补写。
- [ ] 无限滚动用卡片 `Bounds()` 与窗口高度判断触底，阈值是固定 2 行（600 DIP），不是真实可见行数；窗口很矮时会略早预取。
- [x] ~~`ImageStore` 是 FIFO 淘汰而非 LRU~~ 已改为 LRU（命中移到最近使用端），淘汰与 `Retry` 均为 O(1)。
- [x] ~~`go test -race` 在下载 toast 上报竞态~~ 根因是 `App.update` 在无窗口（测试）时直接在当前 goroutine 执行，后台 goroutine 直接调了 `ui.AddToast`，与 `runFrame` 的 `toastViewport` 冲突。改为 `App.toast`/`toastLocked` 只把 toast 入队，由 `View` 构建帧时 `flushToasts` 真正添加，整包 `-race` 通过。
- [ ] 性能基准跑在 headless 的 CPU 软件渲染器上，会比 GPU 路径慢；GPU 下的真实帧率仍需在真机测量。
- [ ] 首页列表的滚动帧（CPU 渲染约 7.4ms）明显慢于缩略图网格（约 0.8ms）：光栅化器对封面图做逐像素采样，占约 88% 的帧时间。实测**与列表长度无关**（100 与 5000 个画廊均约 7ms），GPU 路径不适用；若 Linux 回退 CPU 渲染，可考虑让封面预缩放到接近显示尺寸。
- [x] ~~详情页页面缩略图仍是占位文案。~~ 已接虚拟化缩略图网格，点击跳页。
- [ ] 缩略图网格的选中项没有与阅读进度联动高亮。
- [x] ~~标签没有翻译。~~ EhTagTranslation 数据库已接入，chip 显示中文翻译。
- [x] ~~设置页是占位：深色模式开关、缓存信息都未接真实逻辑。~~ 存储、缓存、主题（跟随系统/浅色/深色）、语言与关于均已接上。
- [x] ~~下载管理/标签设置是空状态页。~~ 下载管理已接 prefill 任务；标签设置已接规则与同步。
- [ ] 错误文案、加载文案全部硬编码中文。
- [ ] 本机 `backend/cmd/desktop/go.mod` 是本地补丁，不在仓库；若删除旧 Wails 目录可移除。
- [ ] Go 模块代理在本环境对 mygo 返回过 502；需要 `GOPROXY=https://proxy.golang.org,direct`。
- [ ] `snapshot_test.go` 需要 `-tags snapshot`，默认测试不会写文件。

---

## 6. v1 验收标准

- [x] 单个可执行文件启动，无外部后端、无 WebView、无 Node/React 构建。（MVP：默认内嵌后端）
- [x] 重复启动聚焦已有窗口；内嵌后端只接受携带正确令牌的本机请求。
- [x] 首次启动可填写 ExHentai cookies，校验通过后进入首页。
- [x] 首页/搜索/详情/阅读器/书架/最近阅读全部可用（列表自动翻页、搜索防抖、详情含缩略图/种子/下载）。
- [x] 阅读器支持翻页、缩放、平移、进度保存、页面缩略图（并有双页、滚轮翻页、跳页、RTL、全屏、四种适应模式）。
- [x] 断网时可读已缓存画廊；缓存有上限并可清理。
- [x] 标签设置、下载已对齐（含下载完成后的离线阅读入口）；数据同步仍待产品决策（见第 4 节）。
- [~] Linux 本机构建运行；macOS/Windows 交叉编译通过，未在真机运行。
- [x] `go test ./...`、`go test -race ./internal/cache/... ./cmd/desktop-native/...`、`go vet ./...` 通过。
- [x] 1000 页画廊滚动帧成本不随列表增长（CPU 软件渲染约 7.4ms/帧），1000 页缩略图网格堆内存约 1.4MB（目标 < 300MB）。GPU 下的真机帧率仍待测。

---

## 7. 继续开发指南

1. 先看 `backend/cmd/desktop-native/README.md`，了解代码结构与运行方式。
2. 跑测试：
   ```bash
   cd backend
   go test ./...
   go test -race ./internal/cache/... ./cmd/desktop-native/...
   go test -tags snapshot ./cmd/desktop-native/app
   # 性能目标：滚动帧预算、内存上限、帧成本不随列表增长
   go test ./cmd/desktop-native/app -run 'TestScroll|TestMemory'
   ```
   计时断言在 `-race` 下会自动跳过（`raceEnabled`），因为竞态检测器会让每帧慢一个数量级。
3. 按 **3.1 P0** 的顺序推进：其余 P1（缩略图/预加载/双页/全屏等）。
4. 每完成一项，把本文件对应复选框改为 `[x]`，并在提交信息里引用里程碑（例如 `feat(desktop): embed API server (M2)`）。
5. 涉及删除旧代码（3.5）前，先在 issue/对话里确认，不要直接删。
