# Manga Reader 原生桌面端

用 [mygo](https://github.com/egoist/mygo) 的 `ui` 包重写的桌面客户端：纯 Go、原生控件、无 WebView、无前端构建步骤。

> 完整的迁移进度、剩余任务和待拍板决策见仓库根目录的 [`MYGO_MIGRATION.md`](../../../MYGO_MIGRATION.md)。

## 当前进度

已完成第一条可运行链路，并已内置后端：

- 原生窗口：侧边栏 + 路由 + 页面切换
- 首页 / 订阅 / 热门 / 搜索 / 书架 / 最近阅读：接真实后端 API，支持分页加载；搜索输入有 300ms 防抖
- 画廊详情：元数据、标签、封面、加入/移出书架、页面缩略图网格（点击跳页）
- 阅读器：真实页面列表、翻页（按钮/方向键/点击左右区域/页码跳转/滚轮）、缩放、拖拽平移、进度保存、相邻页预加载、适合页面/适应宽度、单页/双页、左右阅读方向、全屏
- 异步图片加载：并发下载、后台解码、LRU、加载完成自动重绘
- **内嵌后端**：默认在本进程内启动现有 Gin API（`127.0.0.1` 随机端口），无需外部服务
- **本地图片缓存**：用文件系统实现 `handler.ImageCache`，不再依赖 MinIO；UI 默认通过 `/api/image-cache/*` 读写本地缓存
- **本地数据**：SQLite 与图片缓存默认放在用户配置/缓存目录
- **首次设置**：缺少 Cookies 时显示引导界面，保存后启动后端；设置页可查看路径/缓存占用、设置缓存上限、清空缓存、重新配置 Cookies
- **单实例 + 访问令牌**：重复启动聚焦已有窗口；内嵌后端要求每次启动的随机令牌
- **离线下载**：详情页一键排队，`/downloads` 显示任务状态、进度、失败页数，可取消/删除，完成后导出 ZIP
- **种子**：详情页按需加载 torrents 列表，可保存 `.torrent` 文件
- **标签设置**：喜欢/不喜欢 + 权重规则，列表卡片与详情页标签按规则着色排序并显示中文翻译，支持与源站 My Tags 三路合并同步
- 无后端时显示错误页与「重试」，不会空白或崩溃

暂未实现：逐条冲突选择、数据同步、三平台打包。

## 运行

默认不需要任何外部服务，直接运行：

```bash
cd backend
go run ./cmd/desktop-native
```

首次启动会显示「首次设置」界面，粘贴 ExHentai 的 Cookie 请求头即可。应用会：

1. 把 Cookie 写入 `~/.config/manga-reader/config.json`（macOS 为 `~/Library/Application Support/manga-reader/config.json`，权限 0600）
2. 在 `127.0.0.1` 随机端口启动内嵌后端
3. 把 SQLite 放在用户配置目录、图片缓存放在用户缓存目录

也可以用环境变量跳过引导：`EHENTAI_COOKIE`（完整 cookie 串）或 `EHENTAI_COOKIE_IPB_MEMBER_ID` + `EHENTAI_COOKIE_IPB_PASS_HASH`。可选变量：

- `MANGA_READER_DB_PATH`：SQLite 路径（默认用户配置目录）
- `MANGA_READER_CACHE_DIR`：图片缓存目录（默认用户缓存目录）
- `MANGA_READER_CACHE_MAX_BYTES`：图片缓存上限（字节，`0` 表示不限制；默认 2 GiB）
- `MANGA_READER_SYNC_TOKEN`：启用同步端点
- `MANGA_READER_DEV_TOOLS=true`：启用 `/api/dev/*`
- `MANGA_READER_IMAGE_CACHE=false`：关闭本地图片缓存代理，退回 `/api/image/*` 直连上游（连外部后端且对方没配 MinIO 时用）

「设置」页的主题可在「跟随系统 / 浅色 / 深色」间切换（写入 `config.json`，启动时应用；`MANGA_READER_APPEARANCE` 可覆盖）。「设置」页会显示后端地址、配置文件、数据库、缓存目录与缓存占用，可设置缓存上限（MB，`0` 为不限）、一键清空缓存、重新配置 Cookies。缓存超过上限时，写入会触发后台清理，删除最久未使用的图片直到回落。

进程内后端只监听 `127.0.0.1`，并要求一个每次启动随机生成的访问令牌（UI 客户端与图片请求都会携带），本机其他进程无法直接访问。应用是单实例的：再次启动不会另起后端，而是聚焦已有窗口（通过配置目录下的 `instance.sock` 通信）。

连接外部已运行的后端（开发调试用，不显示引导）：

```bash
MANGA_READER_API=http://127.0.0.1:8080 go run ./cmd/desktop-native
```

如果后端开了 Basic Auth，可以把凭据写进 URL：

```bash
MANGA_READER_API=http://user:pass@127.0.0.1:8080 go run ./cmd/desktop-native
```

## 代码结构

```
cmd/desktop-native/
├── main.go              # mygo 窗口、配置加载、访问令牌、单实例、内嵌后端启动
├── backend.go           # app.Backend 适配器：启动/重启/关闭内嵌后端
├── internal/desktop/
│   ├── settings.go      # config.json 读写、默认数据库/缓存路径
│   └── server.go        # 进程内启动现有 Gin API + 本地 FS 缓存 + 令牌校验
├── internal/singleinstance/
│   └── singleinstance.go # 基于 Unix socket 的单实例锁与"聚焦已有窗口"
└── app/
    ├── app.go           # 应用状态、侧边栏、路由、列表/详情页、首次设置、设置页
    ├── backend.go       # Backend 接口（由 main 实现，测试用假实现）
    ├── client.go        # 后端 HTTP 客户端（列表、详情、NDJSON pages、书架、进度）
    ├── reader.go        # 阅读器状态、翻页、缩放/平移、预加载、进度上报
    ├── downloads.go     # 离线下载任务列表、进度轮询、取消/删除
    ├── zip.go           # 保存对话框 + 流式导出 prefill ZIP
    ├── tags.go          # 标签规则页、着色排序、与源站同步
    ├── torrents.go      # 种子列表、.torrent 保存
    ├── tagtranslation.go # EhTagTranslation 数据库加载、缓存、搜索
    ├── images.go        # 异步下载 + 解码 + LRU
    ├── covers.go        # 占位封面与占位页面（图片加载完成前显示）
    ├── data.go          # UI 侧 Gallery 模型、分类中文名
    ├── app_test.go      # httptest 假后端 + headless Tester 端到端测试
    └── snapshot_test.go # 把主要页面渲染到 /tmp/opencode/*.png
```

`internal/cache/fscache.go` 在复用现有后端时提供 `handler.ImageCache` 的文件系统实现（原子写入、sidecar 元数据、`fs.ErrNotExist` 作为 miss）。可设置容量上限：写入后若超过上限，会异步按修改时间淘汰最旧的图片（数据文件与其 sidecar 一起删除），并保留 10% 余量避免频繁触发。

### 数据流

- 启动时 `internal/desktop.Start` 在随机 loopback 端口起 Gin，UI 客户端指向该地址；`MANGA_READER_API` 可覆盖为外部后端。
- 内嵌模式生成随机令牌（`config.AuthHeader`），API 客户端与 `ImageStore` 都带上它；服务端中间件用常量时间比较校验，缺失或不匹配返回 401。
- 单实例：第一个进程在配置目录监听 `instance.sock`，后续进程连接它并发送 `focus`，然后退出；监听进程调用 `Window.Show()/Focus()` 把窗口带到前台。
- 列表与详情在后台 goroutine 请求，结果通过 `App.update` 主线程回写；视图永不阻塞在网络请求上。
- 图片由 `ImageStore` 按 URL 缓存；`Err` 暴露失败原因，`Retry` 清掉失败项，列表封面和阅读页因此能显示占位与「重试」。`Get` 立即返回 `nil` 或已解码的 bitmap，首次请求在后台进行；完成后调用 `Window.Update` 请求下一帧。
- 阅读器的页面列表走 NDJSON 流式接口，在线失败时回退到 `/api/gallery-cache/...`。
- 阅读器每帧预热当前页前 1 后 2 页（`readerPreloadPages`）：`ImageStore.Get` 对已入队/已加载的页面几乎无开销，对未加载的页面在后台启动下载；可见页优先入队。预加载是幂等的，后续帧重复请求同一 URL 不会重复下载。
- 阅读区的指针事件在 `HandleInput` 里区分点击与拖拽（位移 < 6px 视为点击）：未放大时点左右三分区翻页，拖动则平移；`rtl` 打开后左右方向与方向键互换。
- 双页模式每次步进两页并保持偶数起始页；滚轮可切换「平移/翻页」，平移模式下滚到上下边界也会翻页。
- 底部栏的页码输入框调用 `readerJumpTo`，页码按 1 起算并钳制到有效范围，跳转后上报进度。
- 详情页缩略图网格用 `PageThumbnailURL(id, token, index)`（后端按精灵图坐标切图），只构建可见格；点击格子把目标页写入 `readerStart`，阅读器加载完成后跳到该页并覆盖保存进度。
- 列表在还剩约两行时自动请求下一页（`ensureMore`），底部同时显示加载中/「重试」/「加载更多」。
- 阅读进度在翻页 600ms 后异步上报（批量），离开阅读器时立即补写；详情页按钮显示「继续阅读（第 N 页）」。
- 离线下载：详情页先取页面列表，再 `POST /api/prefill` 建任务；`/downloads` 用 `c.After` 在每 1.5s 触发一次轮询，只轮询 `queued`/`running` 的任务。删除任务时先本地移除并把 ID 记入 `dropped`，避免删除前发出的列表请求把它带回来。
- 标签规则：`Gallery.Tags` 存 `{namespace,name}`，列表接口给的 `"ns:name"` 用 `splitTag` 拆开；卡片最多显示前两个标签。`View` 每次比对 `announcedRules`，规则一变就通知已加载的列表与详情重画，无需重新请求。
- 标签规则：`TagRule` 与后端 `tag_preference` 一一对应，保存是**全量替换**（`PUT /api/tag-settings`）；详情页 `sortedTags` + `tagChip` 按喜欢→不喜欢→其余排序并着色（#3377FF / #FF6666）。同步按钮把「确认删除」「冲突处理」两个输入框翻译成 `confirm_deletes` / `conflict_resolution` 发给后端。
- 标签翻译：数据库取 `db.text.json`（发布里的 `db.text.js` 需要 JS inflate 运行时，原生端没法执行），解析出 `namespace:name → 中文`，缓存为 `<缓存目录上级>/tag-translations.json`，后台加载完成后请求一帧。加载失败会保留旧表。
- 种子：进入详情页不会自动抓取（上游实时抓取较慢），点「查看种子」才加载；保存 `.torrent` 与 ZIP 共用 `Client.downloadTo`。
- ZIP 导出：完成后点「导出 ZIP」，`mygo.Dialog.Save` 选路径（测试里用 `pickSavePath` 变量替换），然后流式 `io.Copy` 到文件，不占内存；失败时删除半成品。
- 搜索输入用 300ms 防抖（`scheduleSearch`）：每次文本变化只保留最后一个定时器，因此连续输入只发一次请求。

## CI

`.github/workflows/native-desktop.yml` 在 Linux 上装 GTK3 跑 `gofmt`/`vet`/`test`/`-race`，并用矩阵交叉编译 linux/amd64、darwin/arm64、darwin/amd64、windows/amd64 的桌面包；Linux 额外产出完整二进制 artifact。

## 测试

```bash
cd backend
go test ./cmd/desktop-native/...
```

测试用 `httptest` 起一个假后端，覆盖首页加载、侧边栏导航、画廊详情、详情页缩略图跳页、阅读器翻页、点击/方向键/跳页/RTL、阅读器预加载、搜索过滤、搜索防抖、离线下载全流程（含 ZIP 导出）、标签规则（增删改、着色、同步）、标签翻译（解析/缓存/chip）、种子列表与下载、阅读器返回。

要把首页/详情/阅读器/搜索渲染成 PNG（输出到 `/tmp/opencode`，便于检查布局和中文字体）：

```bash
go test -tags snapshot ./cmd/desktop-native/app
```

## 下一步

1. **页面缩略图**：解析 `/pages` 的 sprite 坐标，在详情页做虚拟化缩略图网格。
2. **离线缓存预取**：详情页触发 prefill，UI 显示下载进度。
3. **标签设置 / 翻译 / 同步**：按现有后端能力逐项迁移。
4. **打包**：macOS、Linux、Windows 三平台的 mygo 打包与签名。
