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
| 502 | Upstream error (ExHentai API 或抓取失败) |
