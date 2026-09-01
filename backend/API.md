# Manga Reader API

Base URL: `http://localhost:8080`

## Common Response Types

### GalleryCategory

```
"doujinshi" | "manga" | "artistcg" | "gamecg" | "western" | "image-set" | "cosplay" | "asianporn" | "non-h" | "misc" | "other"
```

### Tag

```json
{
  "namespace": "female",
  "name": "yuri"
}
```

---

## Endpoints

### 1. Search

`GET /api/search`

**Query Parameters:**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| q | string | yes | - | Search keyword (空字符串返回全站) |
| site | string | no | `"exhentai"` | `"exhentai"` 或 `"ehentai"` |
| categories | string | no | - | 逗号分隔的分类，如 `"doujinshi,manga"` |
| page | int | no | `0` | 页码 (0-indexed) |
| min_pages | int | no | - | 最小页数 (>= 0) |
| max_pages | int | no | - | 最大页数 (>= 0) |
| min_rating | int | no | - | 最低评分: `2`, `3`, `4`, `5` |
| has_torrent | bool | no | `false` | 仅显示有种子的画廊 |
| include_expunged | bool | no | `false` | 包含已删除的画廊 |
| search_name | bool | no | `false` | 在标题中搜索 |
| search_tags | bool | no | `false` | 在标签中搜索 |
| search_description | bool | no | `false` | 在描述中搜索 |
| include_low_power_tags | bool | no | `false` | 包含低权重标签 |
| include_downvoted_tags | bool | no | `false` | 包含被降权的标签 |
| disable_language_filter | bool | no | `false` | 禁用语言自定义过滤器 |
| disable_uploader_filter | bool | no | `false` | 禁用上传者自定义过滤器 |
| disable_tag_filter | bool | no | `false` | 禁用标签自定义过滤器 |

> **注意**: 当指定任何高级搜索参数时，`advsearch=1` 会自动启用，无需手动传递。

**Validation Rules:**

- `min_pages` 和 `max_pages` 必须 >= 0
- 若同时提供 `min_pages` 和 `max_pages`，则 `min_pages <= max_pages`
- `min_rating` 只允许 `2`, `3`, `4`, `5`
- 布尔参数接受: `true`, `false`, `1`, `0`
- 无效参数返回 HTTP 400

**ExHentai Parameter Mapping:**

| API Parameter | ExHentai Parameter |
|---|---|
| `q` | `f_search` |
| `min_pages` | `f_spf` |
| `max_pages` | `f_spt` |
| `min_rating` | `f_sr=on` + `f_srdd` |
| `has_torrent` | `f_sto=on` |
| `include_expunged` | `f_sh=on` |
| `search_name` | `f_sname=on` |
| `search_tags` | `f_stags=on` |
| `search_description` | `f_sdesc=on` |
| `include_low_power_tags` | `f_sdt1=on` |
| `include_downvoted_tags` | `f_sdt2=on` |
| `disable_language_filter` | `f_sfl=on` |
| `disable_uploader_filter` | `f_sfu=on` |
| `disable_tag_filter` | `f_sft=on` |

**Example:**

```
GET /api/search?q=o:3d$&has_torrent=true&min_pages=10&max_pages=200&min_rating=4
```

等价于 ExHentai 请求:

```
https://exhentai.org/?f_search=o%3A3d%24&advsearch=1&f_sto=on&f_spf=10&f_spt=200&f_sr=on&f_srdd=4
```

**Response (200):**

```json
{
  "total": 100,
  "total_pages": 4,
  "page": 0,
  "page_size": 25,
  "results": [
    {
      "id": 123456,
      "token": "abcdef1234",
      "title": "Gallery Title",
      "title_jpn": "タイトル",
      "category": "doujinshi",
      "cover": "https://example.com/thumb.webp",
      "posted": "2024-01-01",
      "rating": 4.5,
      "url": "https://exhentai.org/g/123456/abcdef1234/",
      "tags": ["female:yuri", "full color"],
      "uploader": "uploader_name",
      "pages": 24,
      "domain": "exhentai.org"
    }
  ]
}
```

---

### 2. Gallery Detail (Official API)

`GET /api/gallery/:id/:token`

**Path Parameters:**

| Name | Type | Description |
|------|------|-------------|
| id | int | Gallery ID |
| token | string | Gallery token |

**Response (200):**

```json
{
  "id": 123456,
  "token": "abcdef1234",
  "title": "Gallery Title",
  "title_jpn": "タイトル",
  "category": "doujinshi",
  "thumbnail": "https://example.com/thumb.webp",
  "page_count": 24,
  "rating": 4.5,
  "rating_count": 100,
  "uploader": "uploader_name",
  "posted_at": "2024-01-01T00:00:00Z",
  "tags": [
    { "namespace": "female", "name": "yuri" }
  ],
  "file_size": "15 MB",
  "expunged": false
}
```

---

### 3. Gallery Detail (Scraped)

`GET /api/gallery/:id/:token/details`

**Path Parameters:**

| Name | Type | Description |
|------|------|-------------|
| id | int | Gallery ID |
| token | string | Gallery token |

**Response (200):**

```json
{
  "id": 123456,
  "token": "abcdef1234",
  "domain": "exhentai.org",
  "title": "Gallery Title",
  "title_jpn": "タイトル",
  "cover": "https://example.com/thumb.webp",
  "category": "doujinshi",
  "uploader": "uploader_name",
  "posted": "2024-01-01",
  "parent": 0,
  "visible": "Yes",
  "language": "English",
  "translated": true,
  "file_size": "15 MB",
  "page_count": 24,
  "favorited": 50,
  "rating_count": 100,
  "rating": 4.5,
  "tags": [
    { "namespace": "female", "name": "yuri" }
  ],
  "page_urls": [
    "https://exhentai.org/s/abcdef1234/123456-1",
    "https://exhentai.org/s/abcdef1234/123456-2"
  ]
}
```

---

### 4. Gallery Page List

`GET /api/gallery/:id/:token/pages`

**Path Parameters:**

| Name | Type | Description |
|------|------|-------------|
| id | int | Gallery ID |
| token | string | Gallery token |

**Response (200):**

```json
{
  "id": "123456",
  "token": "abcdef1234",
  "total": 24,
  "pages": [
    {
      "page_url": "https://exhentai.org/s/abcdef1234/123456-1",
      "index": 0
    },
    {
      "page_url": "https://exhentai.org/s/abcdef1234/123456-2",
      "index": 1
    }
  ]
}
```

---

### 5. Page Image

`GET /api/page-image`

代理 ExHentai 图片，返回原始图片字节。

**Query Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| url | string | yes | 页面 URL (来自 `page_urls` 或 `pages` 数组) |

**Response:**

- 成功: 原始图片数据 (`Content-Type: image/jpeg` 或 `image/png`)
- NL 重试: 初次下载失败时自动重试最多 2 次

**Error Response (502):**

```json
{
  "error": "download image failed: ..."
}
```

---

### 6. Homepage Gallery List

`GET /api/gallerys`

**Query Parameters:**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| page | int | no | `0` | 页码 (0-indexed) |
| min_pages | int | no | - | 最小页数 (>= 0) |
| max_pages | int | no | - | 最大页数 (>= 0) |
| min_rating | int | no | - | 最低评分: `2`, `3`, `4`, `5` |
| has_torrent | bool | no | `false` | 仅显示有种子的画廊 |
| include_expunged | bool | no | `false` | 包含已删除的画廊 |
| search_name | bool | no | `false` | 在标题中搜索 |
| search_tags | bool | no | `false` | 在标签中搜索 |
| search_description | bool | no | `false` | 在描述中搜索 |
| include_low_power_tags | bool | no | `false` | 包含低权重标签 |
| include_downvoted_tags | bool | no | `false` | 包含被降权的标签 |
| disable_language_filter | bool | no | `false` | 禁用语言自定义过滤器 |
| disable_uploader_filter | bool | no | `false` | 禁用上传者自定义过滤器 |
| disable_tag_filter | bool | no | `false` | 禁用标签自定义过滤器 |

**Response (200):**

```json
{
  "page": 0,
  "page_size": 25,
  "results": [
    {
      "id": 123456,
      "token": "abcdef1234",
      "title": "Gallery Title",
      "category": "doujinshi",
      "cover": "https://example.com/thumb.webp",
      "posted": "2024-01-01",
      "rating": 4.5,
      "url": "https://exhentai.org/g/123456/abcdef1234/",
      "tags": ["female:yuri"],
      "uploader": "uploader_name",
      "pages": 24,
      "domain": "exhentai.org"
    }
  ]
}
```

---

### 7. Watched List

`GET /api/watched`

获取关注标签的画廊列表。

**Query Parameters:**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| page | int | no | `0` | 页码 (0-indexed) |
| min_pages | int | no | - | 最小页数 (>= 0) |
| max_pages | int | no | - | 最大页数 (>= 0) |
| min_rating | int | no | - | 最低评分: `2`, `3`, `4`, `5` |
| has_torrent | bool | no | `false` | 仅显示有种子的画廊 |
| include_expunged | bool | no | `false` | 包含已删除的画廊 |
| search_name | bool | no | `false` | 在标题中搜索 |
| search_tags | bool | no | `false` | 在标签中搜索 |
| search_description | bool | no | `false` | 在描述中搜索 |
| include_low_power_tags | bool | no | `false` | 包含低权重标签 |
| include_downvoted_tags | bool | no | `false` | 包含被降权的标签 |
| disable_language_filter | bool | no | `false` | 禁用语言自定义过滤器 |
| disable_uploader_filter | bool | no | `false` | 禁用上传者自定义过滤器 |
| disable_tag_filter | bool | no | `false` | 禁用标签自定义过滤器 |

**Response (200):**

与 `/api/gallerys` 格式相同。

> **注意**: 如果未设置关注标签，返回空列表。ExHentai 的 watched 页面不支持翻页，`page > 0` 始终返回空。

---

### 8. Popular List

`GET /api/popular`

获取热门画廊列表。

**Query Parameters:**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| page | int | no | `0` | 页码 (0-indexed) |

**Response (200):**

与 `/api/gallerys` 格式相同。

> **注意**: ExHentai 的 popular 页面不支持翻页，只有第一页有数据，`page > 0` 始终返回空。

---

### 9. Cached Image

`GET /api/cached-image`

带 MinIO 缓存的图片代理。首次请求从 ExHentai 获取图片并缓存到 MinIO，后续相同 URL 直接从 MinIO 返回。

**Query Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| url | string | yes | 页面 URL (来自 `page_urls` 或 `pages` 数组) |

**Response:**

- 成功: 原始图片数据 (`Content-Type: image/jpeg`, `image/png`, `image/webp`, `image/gif`)
- Cache Hit: 直接从 MinIO 返回，不请求 ExHentai
- Cache Miss: 从 ExHentai 获取，写入 MinIO，返回图片
- 并发保护: 同一 URL 的并发 miss 只触发一次远程请求 (singleflight)

**Headers:**

```
Cache-Control: public, max-age=31536000, immutable
```

**Error Responses:**

```json
{
  "error": "missing url parameter"
}
```

| Status Code | Description |
|-------------|-------------|
| 400 | Bad request (参数无效、URL 不合法) |
| 502 | 上游错误 (ExHentai 请求失败) |
| 503 | Cache 未配置 (MinIO 环境变量缺失) |

**URL 安全限制:**

仅允许 `exhentai.org` 和 `e-hentai.org` 域名，阻止 localhost、RFC1918 私网地址、云 Metadata Service 等内部地址。

---

### 10. Thumbnail Image

`GET /api/thumbnail`

代理 ExHentai Thumbnail 源站图片，返回原始图片字节。**不依赖 MinIO**。

**Query Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| url | string | yes | 缩略图 URL (来自 `cover` / `thumbnail` 字段) |

**Response:**

- 成功: 原始图片数据 (`Content-Type: image/webp`, `image/jpeg`, `image/png`, `image/gif`)
- 根据源站实际 Content-Type 返回，不强制转换
- 失败时自动重试最多 2 次

**Headers:**

```
Cache-Control: public, max-age=3600
```

**Error Responses:**

```json
{
  "error": "invalid thumbnail url"
}
```

| Status Code | Description |
|-------------|-------------|
| 400 | URL 缺失或 URL 非法 |
| 502 | ExHentai Thumbnail 获取失败 |

**URL 安全限制:**

- 仅允许 `https://` 协议，拒绝 `http://`
- 仅允许缩略图域名白名单: `s.exhentai.org`、`ehgt.org`、`ul.e-hentai.org`
- 阻止 localhost、RFC1918 私网地址、Link-local、云 Metadata Service 等内部地址 (SSRF 防护)

> 该 API 仅做源站代理，不负责 MinIO 持久化缓存。

---

### 11. Cached Thumbnail

`GET /api/cached-thumbnail`

带 MinIO 缓存的 Thumbnail 代理。首次请求从 ExHentai 获取并缓存到 MinIO，后续相同 URL 直接从 MinIO 返回。

**Query Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| url | string | yes | 缩略图 URL (来自 `cover` / `thumbnail` 字段) |

**Response:**

- 成功: 原始图片数据 (`Content-Type: image/webp`, `image/jpeg`, `image/png`, `image/gif`)
- Cache Hit: 直接从 MinIO 返回，不请求 ExHentai
- Cache Miss: 从 ExHentai 获取，写入 MinIO，返回图片
- 并发保护: 同一 URL 的并发 miss 只触发一次远程请求 (singleflight)

**Headers:**

```
Cache-Control: public, max-age=31536000, immutable
```

**MinIO Cache:**

- Object Prefix: `thumbnail/`
- Cache Key: `thumbnail/<sha256(完整缩略图 URL)>`
- 保存 Metadata: `Content-Type`、`Content-Length`、`x-amz-meta-source-url` (原始 URL)
- Cache Key 基于完整 URL，URL 变化会生成新的 Object Key，不会污染旧缓存

**Error Responses:**

```json
{
  "error": "missing url parameter"
}
```

| Status Code | Description |
|-------------|-------------|
| 400 | Bad request (参数无效、URL 不合法) |
| 502 | 上游错误 (ExHentai 请求失败) |
| 503 | Cache 未配置 (MinIO 环境变量缺失) |

**URL 安全限制:**

与 `/api/thumbnail` 相同，仅允许 `https://` 及缩略图域名白名单，阻止内网地址。

**前端使用方式:**

```html
<img src="/api/cached-thumbnail?url=https%3A%2F%2Fs.exhentai.org%2Fw%2F00%2F999%2F15582-3owak8q3.webp" />
```

---

### 12. Bookshelf List

`GET /api/bookshelf`

获取书架列表，按收藏时间倒序排列。

**Query Parameters:**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| page | int | no | `0` | 页码 (0-indexed) |

**Response (200):**

```json
{
  "page": 0,
  "page_size": 25,
  "total": 100,
  "total_pages": 4,
  "results": [
    {
      "id": 123456,
      "token": "abcdef1234",
      "title": "Gallery Title",
      "title_jpn": "タイトル",
      "category": "doujinshi",
      "thumbnail": "https://example.com/thumb.webp",
      "pages": 24,
      "added_at": "2026-08-29T10:00:00Z",
      "updated_at": "2026-08-29T10:00:00Z",
      "reading": {
        "gallery_id": 123456,
        "token": "abcdef1234",
        "current_page": 10,
        "progress": 0.416,
        "completed": false,
        "started_at": "2026-08-29T10:00:00Z",
        "updated_at": "2026-08-29T12:00:00Z"
      }
    }
  ]
}
```

> **注意**: `reading` 字段仅在存在阅读进度时出现。

---

### 13. Add to Bookshelf

`POST /api/bookshelf/:id/:token`

将 Gallery 加入书架。首次添加时会从 ExHentai 获取 Gallery 基本信息并保存快照。重复添加幂等，不会产生重复数据。

**Path Parameters:**

| Name | Type | Description |
|------|------|-------------|
| id | int | Gallery ID |
| token | string | Gallery token |

**Response (200):**

```json
{
  "success": true,
  "in_bookshelf": true
}
```

**Error Responses:**

| Status Code | Description |
|-------------|-------------|
| 400 | Gallery ID 或 token 无效 |
| 502 | ExHentai API 请求失败 |

---

### 14. Remove from Bookshelf

`DELETE /api/bookshelf/:id/:token`

从书架移除 Gallery。不会删除阅读进度。

**Path Parameters:**

| Name | Type | Description |
|------|------|-------------|
| id | int | Gallery ID |
| token | string | Gallery token |

**Response (200):**

```json
{
  "success": true,
  "in_bookshelf": false
}
```

---

### 15. Bookshelf Status

`GET /api/bookshelf/:id/:token/status`

查询 Gallery 是否在书架中。

**Path Parameters:**

| Name | Type | Description |
|------|------|-------------|
| id | int | Gallery ID |
| token | string | Gallery token |

**Response (200) - 在书架中:**

```json
{
  "in_bookshelf": true,
  "added_at": "2026-08-29T10:00:00Z"
}
```

**Response (200) - 不在书架中:**

```json
{
  "in_bookshelf": false
}
```

---

### 16. Get Reading Progress

`GET /api/progress/:id/:token`

获取 Gallery 的阅读进度。不存在时返回零值。

**Path Parameters:**

| Name | Type | Description |
|------|------|-------------|
| id | int | Gallery ID |
| token | string | Gallery token |

**Response (200):**

```json
{
  "gallery_id": 123456,
  "token": "abcdef1234",
  "current_page": 10,
  "progress": 0.416,
  "completed": false,
  "started_at": "2026-08-29T10:00:00Z",
  "updated_at": "2026-08-29T12:00:00Z"
}
```

> 无阅读记录时 `started_at` 和 `updated_at` 为 `null`。

---

### 17. Update Reading Progress

`PUT /api/progress/:id/:token`

更新 Gallery 的阅读进度。首次保存自动设置 `started_at`，后续更新只修改 `updated_at`。

**Path Parameters:**

| Name | Type | Description |
|------|------|-------------|
| id | int | Gallery ID |
| token | string | Gallery token |

**Request Body:**

```json
{
  "current_page": 10,
  "progress": 0.416,
  "completed": false
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| current_page | int | yes | 当前阅读页码 (≥ 0) |
| progress | float | yes | 阅读进度 (0.0 ~ 1.0) |
| completed | bool | yes | 是否读完。设为 `true` 时 `progress` 自动设为 `1` |

**Response (200):**

```json
{
  "gallery_id": 123456,
  "token": "abcdef1234",
  "current_page": 10,
  "progress": 0.416,
  "completed": false,
  "started_at": "2026-08-29T10:00:00Z",
  "updated_at": "2026-08-29T12:00:00Z"
}
```

**Error Responses:**

| Status Code | Description |
|-------------|-------------|
| 400 | `current_page < 0` 或 `progress` 不在 0~1 范围内 |

---

### 18. Recently Read

`GET /api/recently-read`

获取最近阅读的 Gallery 列表，按最后阅读时间倒序排列，默认返回 25 条。

不依赖书架，即使 Gallery 已从书架移除，只要存在阅读记录仍会显示。

**Response (200):**

```json
{
  "results": [
    {
      "id": 123456,
      "token": "abcdef1234",
      "title": "Gallery Title",
      "title_jpn": "タイトル",
      "category": "doujinshi",
      "thumbnail": "https://example.com/thumb.webp",
      "pages": 24,
      "reading": {
        "gallery_id": 123456,
        "token": "abcdef1234",
        "current_page": 10,
        "progress": 0.416,
        "completed": false,
        "started_at": "2026-08-29T10:00:00Z",
        "updated_at": "2026-08-29T12:00:00Z"
      }
    }
  ]
}
```

> Gallery 从书架移除后，`title`、`title_jpn`、`category`、`thumbnail`、`pages` 返回空字符串/零值。

---

### 19. Cleanup Reading Progress

`POST /api/reading-progress/cleanup`

主动清理阅读记录。只有调用本 API 才会删除阅读记录，其他 API（如 `/api/recently-read`）不会触发自动清理。

**Query Parameters:**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| days | int | no | `30` | 清理 N 天以前的阅读记录；`0` 表示删除全部 |

**示例:**

```http
POST /api/reading-progress/cleanup
```

默认清理 30 天以前。

```http
POST /api/reading-progress/cleanup?days=7
```

清理 7 天以前。

```http
POST /api/reading-progress/cleanup?days=0
```

删除全部阅读记录。

**Response (200):**

```json
{
  "days": 30,
  "deleted": 12
}
```

| Field | Type | Description |
|-------|------|-------------|
| days | int | 本次实际使用的清理天数 |
| deleted | int | 实际删除的阅读记录数量 |

**Error Responses:**

| Status Code | Description |
|-------------|-------------|
| 400 | `days` 不是非负整数（如 `days=-1` 或 `days=abc`） |
| 500 | 数据库错误 |

> 只删除 `reading_progress`，不影响 Bookshelf / Gallery / MinIO 缓存。

---

## Error Responses

所有错误响应格式：

```json
{
  "error": "error message"
}
```

| Status Code | Description |
|-------------|-------------|
| 400 | Bad request (参数无效) |
| 404 | Resource not found |
| 500 | Internal server error |
| 502 | Upstream error (ExHentai API 或抓取失败) |
