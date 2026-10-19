# Manga Reader 原生桌面端

用 [mygo](https://github.com/egoist/mygo) 的 `ui` 包重写的桌面客户端：纯 Go、原生控件、无 WebView、无前端构建步骤。

> 完整的迁移进度、剩余任务和待拍板决策见仓库根目录的 [`MYGO_MIGRATION.md`](../../../MYGO_MIGRATION.md)。

## 当前进度

已完成第一条可运行链路，并已内置后端：

- 原生窗口：侧边栏 + 路由 + 页面切换
- 首页 / 订阅 / 热门 / 搜索 / 书架 / 最近阅读：接真实后端 API，支持分页加载
- 画廊详情：元数据、标签、封面、加入/移出书架
- 阅读器：真实页面列表、翻页、缩放、拖拽平移、进度保存
- 异步图片加载：并发下载、后台解码、LRU、加载完成自动重绘
- **内嵌后端**：默认在本进程内启动现有 Gin API（`127.0.0.1` 随机端口），无需外部服务
- **本地图片缓存**：用文件系统实现 `handler.ImageCache`，不再依赖 MinIO；UI 默认通过 `/api/image-cache/*` 读写本地缓存
- **本地数据**：SQLite 与图片缓存默认放在用户配置/缓存目录
- 无后端时显示错误页与「重试」，不会空白或崩溃

暂未实现：页面缩略图、离线下载、标签翻译、设置界面（首次启动引导）、数据同步、三平台打包。

## 运行

默认不需要任何外部服务，直接运行：

```bash
cd backend
go run ./cmd/desktop-native
```

首次运行前，在 `~/.config/manga-reader/config.json`（Linux；macOS 为 `~/Library/Application Support/manga-reader/config.json`）写入 ExHentai cookie：

```json
{
  "cookie": "ipb_member_id=...; ipb_pass_hash=...; igneous=...; sk=..."
}
```

也可以用环境变量：`EHENTAI_COOKIE`（完整 cookie 串）或 `EHENTAI_COOKIE_IPB_MEMBER_ID` + `EHENTAI_COOKIE_IPB_PASS_HASH`。可选变量：

- `MANGA_READER_DB_PATH`：SQLite 路径（默认用户配置目录）
- `MANGA_READER_CACHE_DIR`：图片缓存目录（默认用户缓存目录）
- `MANGA_READER_SYNC_TOKEN`：启用同步端点
- `MANGA_READER_DEV_TOOLS=true`：启用 `/api/dev/*`
- `MANGA_READER_IMAGE_CACHE=false`：关闭本地图片缓存代理，退回 `/api/image/*` 直连上游（连外部后端且对方没配 MinIO 时用）

连接外部已运行的后端（开发调试用）：

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
├── main.go              # mygo 窗口、配置加载、内嵌后端启动
├── internal/desktop/
│   ├── settings.go      # config.json 读写、默认数据库/缓存路径
│   └── server.go        # 进程内启动现有 Gin API + 本地 FS 缓存
└── app/
    ├── app.go           # 应用状态、侧边栏、路由、列表/详情页
    ├── client.go        # 后端 HTTP 客户端（列表、详情、NDJSON pages、书架、进度）
    ├── reader.go        # 阅读器状态、翻页、缩放/平移、进度上报
    ├── images.go        # 异步下载 + 解码 + LRU
    ├── covers.go        # 占位封面与占位页面（图片加载完成前显示）
    ├── data.go          # UI 侧 Gallery 模型、分类中文名
    ├── app_test.go      # httptest 假后端 + headless Tester 端到端测试
    └── snapshot_test.go # 把主要页面渲染到 /tmp/opencode/*.png
```

`internal/cache/fscache.go` 在复用现有后端时提供 `handler.ImageCache` 的文件系统实现（原子写入、sidecar 元数据、`fs.ErrNotExist` 作为 miss）。

### 数据流

- 启动时 `internal/desktop.Start` 在随机 loopback 端口起 Gin，UI 客户端指向该地址；`MANGA_READER_API` 可覆盖为外部后端。
- 列表与详情在后台 goroutine 请求，结果通过 `App.update` 主线程回写；视图永不阻塞在网络请求上。
- 图片由 `ImageStore` 按 URL 缓存。`Get` 立即返回 `nil` 或已解码的 bitmap，首次请求在后台进行；完成后调用 `Window.Update` 请求下一帧。
- 阅读器的页面列表走 NDJSON 流式接口，在线失败时回退到 `/api/gallery-cache/...`。
- 阅读进度在翻页时异步上报，不阻塞界面。

## 测试

```bash
cd backend
go test ./cmd/desktop-native/...
```

测试用 `httptest` 起一个假后端，覆盖首页加载、侧边栏导航、画廊详情、阅读器翻页、搜索过滤、阅读器返回。

要把首页/详情/阅读器/搜索渲染成 PNG（输出到 `/tmp/opencode`，便于检查布局和中文字体）：

```bash
go test -tags snapshot ./cmd/desktop-native/app
```

## 下一步

1. **首次启动引导**：设置界面填写 cookies、数据库路径、缓存上限、同步开关，写入配置文件。
2. **页面缩略图**：解析 `/pages` 的 sprite 坐标，在详情页做虚拟化缩略图网格。
3. **离线缓存预取**：详情页触发 prefill，UI 显示下载进度。
4. **标签设置 / 翻译 / 同步**：按现有后端能力逐项迁移。
5. **打包**：macOS、Linux、Windows 三平台的 mygo 打包与签名。
