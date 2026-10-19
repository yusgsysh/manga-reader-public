# Manga Reader 原生桌面端

用 [mygo](https://github.com/egoist/mygo) 的 `ui` 包重写的桌面客户端：纯 Go、原生控件、无 WebView、无前端构建步骤。

## 当前进度

已完成第一条可运行链路：

- 原生窗口：侧边栏 + 路由 + 页面切换
- 首页 / 订阅 / 热门 / 搜索 / 书架 / 最近阅读：接真实后端 API，支持分页加载
- 画廊详情：元数据、标签、封面、加入/移出书架
- 阅读器：真实页面列表、翻页、缩放、拖拽平移、进度保存
- 异步图片加载：并发下载、后台解码、LRU、加载完成自动重绘
- 无后端时显示错误页与「重试」，不会空白或崩溃

暂未实现：页面缩略图、离线下载、标签翻译、设置持久化、数据同步、内嵌后端进程、三平台打包。

## 运行

先按仓库根目录的说明启动后端（需要 ExHentai cookies）。然后：

```bash
cd backend
MANGA_READER_API=http://127.0.0.1:8080 go run ./cmd/desktop-native
```

`MANGA_READER_API` 不设置时默认 `http://127.0.0.1:8080`。如果后端开了 Basic Auth，可以把凭据写进 URL：

```bash
MANGA_READER_API=http://user:pass@127.0.0.1:8080 go run ./cmd/desktop-native
```

## 代码结构

```
cmd/desktop-native/
├── main.go              # mygo 窗口与启动
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

### 数据流

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

1. **内嵌后端**：在 mygo 进程内启动现有 Gin（`127.0.0.1` 随机端口），把 `MANGA_READER_API` 从外部依赖变成内部细节。
2. **本地图片缓存**：用文件系统实现 `handler.ImageCache`，去掉 MinIO 依赖，改用 `/api/image-cache/*`。
3. **设置页**：把 cookies、数据库路径、缓存上限、同步开关写成配置文件，首次启动引导填写。
4. **页面缩略图**：解析 `/pages` 的 sprite 坐标，在详情页做虚拟化缩略图网格。
5. **离线下载 / 标签翻译 / 同步**：按现有后端能力逐项迁移。
6. **打包**：macOS、Linux、Windows 三平台的 mygo 打包与签名。
