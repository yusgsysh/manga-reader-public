# Manga Reader 原生桌面端

用 [mygo](https://github.com/egoist/mygo) 的 `ui` 包重写的桌面客户端：纯 Go、原生控件、无 WebView、无前端构建步骤。

> 完整的迁移进度、剩余任务和待拍板决策见仓库根目录的 [`MYGO_MIGRATION.md`](../../../MYGO_MIGRATION.md)。

## 当前进度

已完成第一条可运行链路，并已内置后端：

- 原生窗口：侧边栏 + 路由 + 页面切换
- 首页 / 订阅 / 热门 / 搜索 / 书架 / 最近阅读：接真实后端 API，支持分页加载；搜索输入有 300ms 防抖
- 画廊详情：元数据、标签、封面、加入/移出书架
- 阅读器：真实页面列表、翻页、缩放、拖拽平移、进度保存、相邻页预加载、全屏
- 异步图片加载：并发下载、后台解码、LRU、加载完成自动重绘
- **内嵌后端**：默认在本进程内启动现有 Gin API（`127.0.0.1` 随机端口），无需外部服务
- **本地图片缓存**：用文件系统实现 `handler.ImageCache`，不再依赖 MinIO；UI 默认通过 `/api/image-cache/*` 读写本地缓存
- **本地数据**：SQLite 与图片缓存默认放在用户配置/缓存目录
- **首次设置**：缺少 Cookies 时显示引导界面，保存后启动后端；设置页可查看路径/缓存占用、设置缓存上限、清空缓存、重新配置 Cookies
- **单实例 + 访问令牌**：重复启动聚焦已有窗口；内嵌后端要求每次启动的随机令牌
- 无后端时显示错误页与「重试」，不会空白或崩溃

暂未实现：页面缩略图、离线下载、标签翻译、数据同步、三平台打包。

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

「设置」页会显示后端地址、配置文件、数据库、缓存目录与缓存占用，可设置缓存上限（MB，`0` 为不限）、一键清空缓存、重新配置 Cookies。缓存超过上限时，写入会触发后台清理，删除最久未使用的图片直到回落。

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
- 图片由 `ImageStore` 按 URL 缓存。`Get` 立即返回 `nil` 或已解码的 bitmap，首次请求在后台进行；完成后调用 `Window.Update` 请求下一帧。
- 阅读器的页面列表走 NDJSON 流式接口，在线失败时回退到 `/api/gallery-cache/...`。
- 阅读器每帧预热当前页前 1 后 2 页（`readerPreloadPages`）：`ImageStore.Get` 对已入队/已加载的页面几乎无开销，对未加载的页面在后台启动下载；可见页优先入队。预加载是幂等的，后续帧重复请求同一 URL 不会重复下载。
- 阅读进度在翻页时异步上报，不阻塞界面。
- 搜索输入用 300ms 防抖（`scheduleSearch`）：每次文本变化只保留最后一个定时器，因此连续输入只发一次请求。

## 测试

```bash
cd backend
go test ./cmd/desktop-native/...
```

测试用 `httptest` 起一个假后端，覆盖首页加载、侧边栏导航、画廊详情、阅读器翻页、阅读器预加载、搜索过滤、搜索防抖、阅读器返回。

要把首页/详情/阅读器/搜索渲染成 PNG（输出到 `/tmp/opencode`，便于检查布局和中文字体）：

```bash
go test -tags snapshot ./cmd/desktop-native/app
```

## 下一步

1. **页面缩略图**：解析 `/pages` 的 sprite 坐标，在详情页做虚拟化缩略图网格。
2. **离线缓存预取**：详情页触发 prefill，UI 显示下载进度。
3. **标签设置 / 翻译 / 同步**：按现有后端能力逐项迁移。
4. **打包**：macOS、Linux、Windows 三平台的 mygo 打包与签名。
