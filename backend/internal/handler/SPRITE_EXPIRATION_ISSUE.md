# ExHentai 精灵图过期导致缩略图加载失败 - 问题分析与修复

> 状态：**已修复**（`fix(backend): recover expired exhentai sprite thumbnails`）。
> 本文档保留问题背景与修复前的失败分析，并记录最终实现与验证方式。

## 问题背景

ExHentai 画廊缩略图使用精灵图存储：多张缩略图打包在一张大图中，通过 CSS `background-position` 裁剪显示。每个页面的缩略图信息包含：
- `sprite_url`: 精灵图 URL
- `x`, `y`, `width`, `height`: 裁剪坐标和尺寸

后端通过 MinIO 缓存精灵图（key = SHA256(sprite_url)），缩略图请求时读取精灵图并裁剪返回。

## 问题现象

访问 `https://exhentai.09270721.xyz/gallery/4226662/279b8b71b3`：

| 端点 | Sprite URL | 状态 |
|------|-----------|------|
| Live `/api/gallery/.../pages` | `rlx05k78ssqua7197v` | ✅ 200 |
| Cache `/api/gallery-cache/.../pages` | `f76qapwoio4xnu197r` / `5ego51pp6rxawf197r` | ❌ 404 (过期) |
| Thumbnail `/api/image-cache/page-thumbnail?index=20` | 使用缓存的过期 sprite | ❌ 502 |

**根因**：Gallery cache 存储的 sprite URL 有过期时间（通常几小时），缓存未失效时前端读取到过期 URL，导致缩略图加载失败。

## 修复前架构

```
Frontend (PageThumbnailGrid)
    → GET /api/image-cache/page-thumbnail?id={id}&token={token}&index={index}
        → handleCachedPageThumbnail
            → resolveGalleryPageThumb (从 gallery_cache 读取 sprite 几何信息)
            → loadOrCropThumbnail
                → loadOrFetchSprite (从 MinIO 读取 sprite，miss 时回源)
                    → fetchThumbnail -> ProxyImage (请求 upstream sprite URL)
                        → upstream 返回 404 (sprite 过期)
                        → 错误向上传播，最终返回 502
```

## 复现测试

在 `page_thumbnail_test.go` 中添加了两个测试用例：

### TestCachedPageThumbnail_SpriteExpired_RefetchAndRetry
- Pre-seed gallery_cache with OLD sprite URL
- Mock upstream: first sprite fetch (OLD URL) → 404, gallery re-scrape → NEW sprite URL, second sprite fetch (NEW URL) → 200
- Request thumbnail by index
- Expect: 200 OK, new sprite cached, gallery_cache updated

### TestCachedPageThumbnail_SpriteExpired_ConcurrentRequests
- Same setup, fire N concurrent requests
- Expect: only 1 gallery re-scrape, all requests succeed

## 修复前失败现象（保留作背景）

测试运行时，`refreshPages` 触发但 gallery cache 未更新。日志显示：
```
INFO sprite expired, triggering gallery cache refresh galleryID=999001 token=sprite-expire-retry
WARN timed out waiting for sprite URL update galleryID=999001 token=sprite-expire-retry
WARN gallery page-thumbnail cache read failed id=999001 error="context deadline exceeded"
```

观察到的行为：
1. 初始 resolve 从 gallery_cache 读取到旧 sprite URL，不触发 upstream scrape
2. sprite fetch 返回 404 后，handler 触发 `refreshPages` (go routine)
3. `refreshPages` 通过 `galleryPagesFillGroup.Do` + `galleryPagesHub` 两层 singleflight
4. 背景 goroutine 使用 `context.Background()` 执行 scrape
5. handler 主线程轮询 `resolveGalleryPageThumb` 等待 cache 更新
6. 轮询超时（15s），cache 仍为旧 sprite URL
7. 最终重试仍使用旧 URL，返回 502

## 关键阻塞点（修复前分析）

1. **Refresh 触发时机**：初始 resolve 从 cache 读取到旧 sprite URL，不触发 scrape；refresh 时需强制重新 scrape
2. **Singleflight 去重**：`galleryPagesFillGroup` 和 `galleryPagesHub` 两层去重可能导致 refresh 复用旧 stream，无法触发新 scrape
3. **Cache 更新可见性**：refresh 完成写入 cache 后，handler 需能立即读到新数据
4. **Context 生命周期**：背景 goroutine 使用 `context.Background()` 与请求的 context 生命周期冲突

## 修复方案（已实现）

1. **错误携带 HTTP 状态码**（`internal/exhentai/error.go`、`image.go`、`api.go`）
   `ProxyImage`/`postGalleryMetadata` 返回 `&httpStatusError{code: ...}`，使 `HTTPStatusCode` 与 `IsPermanentUpstreamError` 对 4xx 生效。此前 `httpStatusError` 在生产代码中从未构造，导致 404 被当作瞬态错误。

2. **不再重试永久性 4xx**（`internal/handler/image.go`）
   `fetchThumbnail` 首次请求即对 `IsPermanentUpstreamError` 短路，一次过期 sprite 只打 1 次上游（与测试期望一致）。

3. **有界恢复 + 按索引合并**（`internal/handler/page_thumbnail.go`）
   按索引的整条 resolve + crop + 恢复流程用 `pageThumbGroup`（key = `galleryID:token:index`）合并。缓存命中但 sprite 404 时：
   1. `refreshPagesSync` 全量刷新 gallery cache（写完后返回）；
   2. 重新从 cache 解析；
   3. 若 sprite URL 未变（共享文档去重窗口内仍是旧文档），用 `ScrapeGalleryPageThumb`（`httpGetDocDirect`，绕过 10s 文档去重）直连抓取新 URL 并回填；
   4. 重试一次，否则返回原 502。
   这样并发请求共享一次刷新、一次直连抓取、一次旧/新 sprite 下载。

4. **同步刷新辅助**（`internal/handler/gallery.go`）
   新增 `refreshPagesSync`：复用 `galleryPagesFillGroup` + `scrapeAndCache`，返回前保证缓存已写入，超时受调用方 ctx 约束。

## 页面缓存（同类问题排查与后续加固）

- **页面图片缓存**（`/api/image-cache/page`）：MinIO key = SHA256(稳定的页面 URL)，临时的图片 URL 从不落盘，每次 miss 重新抓页面取图，**不存在同类过期问题**。
- **页面列表缓存**（`/api/gallery-cache/.../pages`）：页面 URL 稳定；缩略图几何部分由上述恢复处理。若页面 URL 轮换且总数不变，旧 URL 会 502。
- **加固（已实现）**：`ScrapePageImageURL` 对非 200 返回 `&httpStatusError{...}`，从而激活已有的 `triggerPageRefresh`，让永久失败的页面 URL 触发 gallery 缓存刷新。

## 运行测试

```bash
cd /home/abc/manga-reader/backend
go test -v -run "TestCachedPageThumbnail_SpriteExpired" ./internal/handler/
go test -count=1 ./internal/handler/ ./internal/exhentai/
```

预期：全部通过（两个 SpriteExpired 用例各只打印一次 `cached sprite expired`，证明并发已合并）。

## 相关文件

- `internal/handler/page_thumbnail.go` - handleCachedPageThumbnail, loadCachedIndexThumbnail, loadOrFetchSprite
- `internal/handler/gallery.go` - refreshPages, refreshPagesSync, scrapeAndCache
- `internal/handler/page_thumbnail_test.go` - 复现与回归测试
- `internal/handler/gallery_cache_test.go` - 页面 URL 自愈测试
- `internal/exhentai/image.go` - ProxyImage, ScrapePageImageURL (状态码分类)
- `internal/exhentai/error.go` - HTTPStatusCode / IsPermanentUpstreamError
- `internal/exhentai/gallery.go` - StreamGalleryPages, ScrapeGalleryPageThumb, extractGalleryPages
