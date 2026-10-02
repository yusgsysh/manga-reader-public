# ExHentai 精灵图过期导致缩略图加载失败 - 问题分析与复现测试

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

## 当前架构

```
Frontend (PageThumbnailGrid)
    → GET /api/image-cache/page-thumbnail?id={id}&token={token}&index={index}
        → handleCachedPageThumbnail (page_thumbnail.go:192)
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

## 失败现象分析

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

## 关键阻塞点

1. **Refresh 触发时机**：初始 resolve 从 cache 读取到旧 sprite URL，不触发 scrape；refresh 时需强制重新 scrape
2. **Singleflight 去重**：`galleryPagesFillGroup` 和 `galleryPagesHub` 两层去重可能导致 refresh 复用旧 stream，无法触发新 scrape
3. **Cache 更新可见性**：refresh 完成写入 cache 后，handler 的 polling loop 需能立即读到新数据
4. **Context 生命周期**：背景 goroutine 使用 `context.Background()` 与测试主线程的 context 生命周期冲突

## 运行复现测试

```bash
cd /home/abc/manga-reader/backend
go test -v -run "TestCachedPageThumbnail_SpriteExpired" ./internal/handler/
```

预期：测试失败，展示上述超时行为。

## 相关文件

- `internal/handler/page_thumbnail.go` - handleCachedPageThumbnail, loadOrFetchSprite
- `internal/handler/gallery.go` - refreshPages, scrapeAndCache
- `internal/handler/page_thumbnail_test.go` - 复现测试用例
- `internal/exhentai/gallery.go` - StreamGalleryPages, extractGalleryPages
- `internal/exhentai/error.go` - HTTPStatusCode 错误码提取