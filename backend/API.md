# Manga Reader API

Base URL: `http://localhost:8080`

## 分页与列表加载

所有列表端点（Search / Homepage / Watched / Popular / Bookshelf / Recently Read）均以 `page`（从 `0` 开始）分页，客户端可独立请求任意页。上游每页 25 条；响应中的 `page_size` 是**实际返回条数**（末页可能少于 25）。

Search / Homepage / Watched / Popular 的上游（ExHentai）使用 `next=<gallery-id>` 游标翻页，并不支持 `?page=N`。服务端会按列表 URL 缓存游标链，因此 `page=0,1,2...` 能稳定映射到不同页，且顺序翻页（无限滚动）每页只需一次上游请求。

前端列表页在此基础上实现无限滚动：

- 滚动接近底部时自动按 `page=0,1,2...` 递增请求并追加结果，不再提供上一页 / 下一页按钮。
- 通过 `IntersectionObserver`（`rootMargin: 0px 0px 200% 0px`，约提前 2 屏）预取下一页，期间不显示加载动画，即“提前加载”。
- 缩略图使用 `loading="lazy"` 懒加载，接近视口时才请求 `/api/image-cache/thumbnail`。

是否还有下一页按端点元信息判断：Search / Bookshelf / Recently Read 使用 `total` / `total_pages`；Homepage / Watched / Popular 按上游每页 25 条的固定页大小判断。

### Jump/Seek

Search / Homepage / Watched 支持上游 ExHentai 的 Jump/Seek 定位，通过两个可选查询参数：

| 参数 | 含义 | 示例 |
|------|------|------|
| `seek` | 定位到指定日期 | `seek=2020`、`seek=2020-01`、`seek=20-01-01` |
| `jump` | 相对当前位置偏移 | `jump=3d`、`jump=1w`、`jump=6m`、`jump=1y` |

- 传 `seek` / `jump` 时，从该定位点开始返回结果；可与 `page` 组合，`page` 表示从定位点向后的第 N 页（0-indexed，用于无限滚动续页）。
- 参数格式非法返回 HTTP 400。`seek` 允许 `YYYY` / `YY-MM` / `YYYY-MM-DD`；`jump` 为数字加可选单位 `d`/`w`/`m`/`y`（或 `-`）。
- 上述三个端点的响应都会附带 `nav` 对象，供前端构建 Jump/Seek UI：

```json
"nav": {
  "prev": "1814200",
  "next": "1813761",
  "min_date": "2007-03-20",
  "max_date": "2026-10-01",
  "range_min": 74,
  "range_max": 74,
  "range_span": 2
}
```

`prev` / `next` 为相邻页的游标 id（空字符串表示没有相邻页）；`min_date` / `max_date` 是可定位的日期范围；`range_min` / `range_max` / `range_span` 描述上游日期滑块的当前位置与刻度。无导航栏的列表（如空的 Watched、Popular）返回零值。

> Popular 不支持 Jump/Seek，请求中的 `seek` / `jump` 不会生效，`nav` 始终为零值。

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

### ListingNav（Jump/Seek 元数据）

由 Search / Homepage / Watched 的响应 `nav` 字段返回，用于构建 Jump/Seek UI。

```json
{
  "prev": "1814200",
  "next": "1813761",
  "min_date": "2007-03-20",
  "max_date": "2026-10-01",
  "range_min": 74,
  "range_max": 74,
  "range_span": 2
}
```

### GalleryPageThumb（页面缩略图精灵图坐标）

由 `/api/gallery/:id/:token/pages` 每页的 `thumbnail` 字段返回；用 `sprite_url` + `x/y/width/height` 调用 `/api/image-cache/page-thumbnail` 获取单张缩略图。

```json
{
  "sprite_url": "https://cdn.hath.network/c2/hash/123456-0.webp",
  "x": 0,
  "y": 0,
  "width": 200,
  "height": 282
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
| seek | string | no | - | Jump/Seek: 定位到日期，如 `2020`、`2020-01`、`20-01-01` |
| jump | string | no | - | Jump/Seek: 相对偏移，如 `3d`、`1w`、`6m`、`1y` |
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
| `page` | 上游 `next=<id>` 游标（服务端解析并缓存，见「分页与列表加载」） |
| `seek` | `seek` |
| `jump` | `jump` |

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
  ],
  "nav": {
    "prev": "1814200",
    "next": "1813761",
    "min_date": "2007-03-20",
    "max_date": "2026-10-01",
    "range_min": 0,
    "range_max": 0,
    "range_span": 2
  }
}
```

> `nav` 见「分页与列表加载 › Jump/Seek」。

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

> 在线成功后会把结果写入 `gallery_cache`（见文末「离线缓存」）。本接口不返回缓存回退，回退由前端调用 `/api/gallery-cache/*` 完成。

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
  ]
}
```

> 完整的页面 URL 列表请使用 `/pages` 接口（见第 4 节）。`details` 只抓取 gallery 首页，单次上游请求即可返回。
> 在线成功后会把结果写入 `gallery_cache`；本接口不做缓存回退。

---

### 4. Gallery Page List

`GET /api/gallery/:id/:token/pages`

**Path Parameters:**

| Name | Type | Description |
|------|------|-------------|
| id | int | Gallery ID |
| token | string | Gallery token |

**Response (200): NDJSON 流**

`Content-Type: application/x-ndjson; charset=utf-8`，`Cache-Control: no-store`。响应为**流式 chunked 传输**：每抓到一页立即写出一行，无需等待整份列表抓取完成。每行是一个 JSON 对象，按顺序：

| 行类型 | 字段 | 说明 |
|--------|------|------|
| `meta` | `id`、`token`、`total` | 首行；`total` 为源站 `.gpc` 的总图片数（解析不到时为 `0`） |
| `page` | `page_url`、`index`、`thumbnail?` | 每页一行；`index` 从 `0` 递增；`thumbnail` 见下 |
| `done` | `total` | 成功终止行；`total` 为实收条数（等于 `page` 行数） |
| `error` | `error` | 失败终止行；客户端必须**丢弃已收到的全部 `page` 行** |

```json
{"type":"meta","id":"123456","token":"abcdef1234","total":24}
{"type":"page","page_url":"https://exhentai.org/s/abcdef1234/123456-1","index":0,"thumbnail":{"sprite_url":"https://cdn.hath.network/c2/hash/123456-0.webp","x":0,"y":0,"width":200,"height":282}}
{"type":"page","page_url":"https://exhentai.org/s/abcdef1234/123456-2","index":1,"thumbnail":{"sprite_url":"https://cdn.hath.network/c2/hash/123456-0.webp","x":200,"y":0,"width":200,"height":282}}
{"type":"done","total":24}
```

**错误语义（成功 / 失败，没有中间状态）：**

- 参数非法：`400` + `{"error": ...}`（普通 JSON）。
- 首个上游文档加载失败（尚未发出任何行）：`502` + `{"error": ...}`（普通 JSON）。
- 中途失败（`meta` 行已在传输中）：HTTP 状态保持 `200`，以 `{"type":"error","error":...}` 终止行报告；此前发出的 `page` 行全部作废，前端应回退到 `gallery-cache` 端点。

**缓存：** 只有**完整成功**（所有缩略图页无错误、且抓到的页数不少于 `meta.total`）才把列表写入 `gallery_cache`；任何失败都**不写入**，缓存中不会出现部分数据。

> `thumbnail` 描述该页缩略图在精灵图中的位置（源站用一张大图 + CSS `background-position` 切割）。用 `sprite_url` + `x/y/width/height` 调用 `/api/image-cache/page-thumbnail` 获取单张缩略图。无缩略图元数据时该字段省略。

---

### 5. Image (live)

图片源站代理，**不依赖 MinIO**，每次请求都从上游获取。缓存版本见第 9-11 节（`/api/image-cache/*`）。

#### Page Image

`GET /api/image/page`

代理 ExHentai 图片，返回原始图片字节。

**Query Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| url | string | yes | 页面 URL (来自 `pages` 数组的 `page_url`) |

**Response:**

- 成功: 原始图片数据 (`Content-Type: image/jpeg` / `image/png` / `image/webp` / `image/gif`)
- NL 重试: 初次下载失败时自动重试最多 2 次
- Headers: `Cache-Control: public, max-age=3600`

**Error Response:**

- `400`: 参数缺失/非法
- `502`: 上游下载失败

```json
{
  "error": "download image failed: ..."
}
```

#### Thumbnail

`GET /api/image/thumbnail`

代理 ExHentai 封面缩略图，返回原始图片字节。**不依赖 MinIO**。

**Query Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| url | string | yes | 缩略图 URL (来自 `cover` / `thumbnail` 字段) |

**Response:**

- 成功: 原始图片数据，按源站实际 `Content-Type` 返回，不强制转换
- 失败时自动重试最多 2 次
- Headers: `Cache-Control: public, max-age=3600`

**URL 安全限制:** 仅允许 `https://`，主机白名单 `s.exhentai.org`、`ehgt.org`、`ul.e-hentai.org`；阻止内网地址。

**Error Response:** `400` 参数/URL 非法；`502` 上游失败。

#### Page Thumbnail

`GET /api/image/page-thumbnail`

从 ExHentai 页面缩略图精灵图中裁出单张缩略图并返回 WebP。源站用一张大图（精灵图）承载多页缩略图，本接口在服务端解码、裁剪、重新编码。支持两种寻址方式：

- **直接裁剪**：`url` + `x`/`y`/`w`/`h`（前端从 `pages[].thumbnail` 取矩形）。
- **按索引**：`id` + `token` + `index`（服务端抓取该页所属精灵图并裁剪，无需先请求 `/pages`）。

**Query Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| url | string | 二选一 | 精灵图 URL（直接裁剪模式） |
| x | int | 二选一 | 裁剪起点 X（>= 0） |
| y | int | 二选一 | 裁剪起点 Y（>= 0） |
| w | int | 二选一 | 裁剪宽度（1..4096） |
| h | int | 二选一 | 裁剪高度（1..4096） |
| id | int | 二选一 | Gallery ID（索引模式） |
| token | string | 二选一 | Gallery token（索引模式） |
| index | int | 二选一 | 页索引 (0-indexed)（索引模式） |

**Response (200):**

- `Content-Type: image/webp`，`Cache-Control: public, max-age=3600`
- 裁剪后会去掉四周完全透明的内边距（源站把页面缩略图居中放入精灵图单元，留有透明边），返回的图片尺寸因此可能小于请求的 `w`/`h`，避免显示时出现白边。

**URL 安全限制（直接裁剪）:** 仅允许 `https`，主机后缀白名单 `hath.network`、`e-hentai.org`、`exhentai.org`、`ehgt.org`；阻止内网地址。

**Error Response:**

- `400`: 参数缺失/非法、URL 不在白名单、裁剪矩形超出精灵图范围、未提供 `url` 或 `id`+`token`
- `404`: 索引超出范围或该页无缩略图元数据
- `502`: 上游下载或裁剪失败

```json
{
  "error": "crop rectangle out of bounds: ..."
}
```

---

### 6. Homepage Gallery List

`GET /api/galleries`

**Query Parameters:**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| page | int | no | `0` | 页码 (0-indexed) |
| seek | string | no | - | Jump/Seek: 定位到日期，如 `2020`、`2020-01` |
| jump | string | no | - | Jump/Seek: 相对偏移，如 `3d`、`1w`、`6m`、`1y` |
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
  ],
  "nav": {
    "prev": "1814200",
    "next": "1813761",
    "min_date": "2007-03-20",
    "max_date": "2026-10-01",
    "range_min": 0,
    "range_max": 0,
    "range_span": 2
  }
}
```

> `nav` 见「分页与列表加载 › Jump/Seek」。

---

### 7. Watched List

`GET /api/watched`

获取关注标签的画廊列表。

**Query Parameters:**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| page | int | no | `0` | 页码 (0-indexed) |
| seek | string | no | - | Jump/Seek: 定位到日期，如 `2020`、`2020-01` |
| jump | string | no | - | Jump/Seek: 相对偏移，如 `3d`、`1w`、`6m`、`1y` |
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

与 `/api/galleries` 格式相同（含 `nav`）。支持 `seek` / `jump` Jump/Seek 定位。

> **注意**: 如果未设置关注标签，返回空列表，且 `nav` 为零值。

---

### 8. Popular List

`GET /api/popular`

获取热门画廊列表。

**Query Parameters:**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| page | int | no | `0` | 页码 (0-indexed) |

**Response (200):**

与 `/api/galleries` 格式相同（含 `nav`）。

> **注意**: ExHentai 的 popular 页面不支持翻页与 Jump/Seek，只有第一页有数据，`page > 0` 始终返回空，`nav` 为零值。

---

### 9. Cached Page Image

`GET /api/image-cache/page`

带 MinIO 缓存的阅读页图片代理。首次请求从 ExHentai 获取图片并缓存到 MinIO，后续相同 URL 直接从 MinIO 返回。live 版本见第 5 节 `/api/image/page`。

**Query Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| url | string | yes | 页面 URL (来自 `pages` 数组的 `page_url`) |

**Response:**

- 成功: 原始图片数据 (`Content-Type: image/jpeg`, `image/png`, `image/webp`, `image/gif`)
- Cache Hit: 直接从 MinIO 返回，不请求 ExHentai
- Cache Miss: 从 ExHentai 获取，写入 MinIO，返回图片
- 并发保护: 同一 URL 的并发 miss 只触发一次远程请求 (singleflight)

**Headers:**

```
Cache-Control: public, max-age=31536000, immutable
```

**MinIO Cache:**

- Object Prefix: `images/`
- Cache Key: `images/<sha256(完整页面 URL)>`

**Error Responses:**

| Status Code | Description |
|-------------|-------------|
| 400 | Bad request (参数无效、URL 不合法) |
| 502 | 上游错误 (ExHentai 请求失败) |
| 503 | Cache 未配置 (MinIO 环境变量缺失) |

**URL 安全限制:**

仅允许 `exhentai.org` 和 `e-hentai.org` 域名，阻止 localhost、RFC1918 私网地址、云 Metadata Service 等内部地址。

---

### 10. Cached Thumbnail Image

`GET /api/image-cache/thumbnail`

带 MinIO 缓存的封面缩略图代理。首次请求从 ExHentai 获取并缓存到 MinIO，后续相同 URL 直接从 MinIO 返回。live 版本见第 5 节 `/api/image/thumbnail`。

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

**Error Responses:**

| Status Code | Description |
|-------------|-------------|
| 400 | Bad request (参数无效、URL 不合法) |
| 502 | 上游错误 (ExHentai 请求失败) |
| 503 | Cache 未配置 (MinIO 环境变量缺失) |

**URL 安全限制:**

与 `/api/image/thumbnail` 相同，仅允许 `https://` 及缩略图域名白名单，阻止内网地址。

**前端使用方式:**

```html
<img src="/api/image-cache/thumbnail?url=https%3A%2F%2Fs.exhentai.org%2Fw%2F00%2F999%2F15582-3owak8q3.webp" />
```

---

### 11. Cached Page Thumbnail

`GET /api/image-cache/page-thumbnail`

带 MinIO 缓存的页面缩略图裁剪，寻址方式与 `/api/image/page-thumbnail` 相同（`url`+矩形 或 `id`+`token`+`index`）。

**行为:**

- **直接裁剪**：MinIO read-through，精灵图 `page-sprite/<sha256(url)>`、裁剪结果 `page-thumb/<sha256(url|x|y|w|h)>`。
- **按索引**：几何优先取 `gallery_cache` 的 `pages[].thumbnail`，未命中则回源抓取；图片走 MinIO read-through。

**Headers:**

```
Cache-Control: public, max-age=31536000, immutable
```

**Error Responses:**

| Status Code | Description |
|-------------|-------------|
| 400 | 参数缺失/非法、URL 不在白名单、裁剪越界 |
| 404 | 索引超出范围或该页无缩略图元数据 |
| 502 | 上游抓取或裁剪失败 |
| 503 | Cache 未配置 (MinIO 环境变量缺失) |

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

支持离线收藏：当 ExHentai 不可达时，若 `gallery_cache` 中已有该 Gallery 的快照（例如之前浏览/下拉过），则使用缓存元数据完成添加，响应中 `offline` 为 `true`；若既无法访问 ExHentai 也没有缓存，则返回 502。

**Path Parameters:**

| Name | Type | Description |
|------|------|-------------|
| id | int | Gallery ID |
| token | string | Gallery token |

**Response (200):**

```json
{
  "success": true,
  "in_bookshelf": true,
  "offline": true
}
```

> `offline` 仅在离线回退成功时出现（`omitempty`）。

**Error Responses:**

| Status Code | Description |
|-------------|-------------|
| 400 | Gallery ID 或 token 无效 |
| 502 | ExHentai API 请求失败且无可用缓存快照 |

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

> 阅读记录只保存进度本身（`current_page`/`progress`/`completed`/`started_at`/`updated_at`）。「最近阅读」所需的元数据来自 `gallery_cache`（见文末「离线缓存」）。

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

获取最近阅读的 Gallery 列表，按最后阅读时间倒序排列，每页 25 条。

不依赖书架，即使 Gallery 已从书架移除，只要存在阅读记录仍会显示。

**Query Parameters:**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| page | int | no | `0` | 页码 (0-indexed) |

**Validation Rules:**

- `page` 必须 >= 0
- 无效参数返回 HTTP 400

**示例:**

```http
GET /api/recently-read?page=0
```

返回第 1 ~ 25 条记录。

```http
GET /api/recently-read?page=1
```

返回第 26 ~ 50 条记录。

不传 `page` 等价于 `page=0`。

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

| Field | Type | Description |
|-------|------|-------------|
| page | int | 当前页码 (0-indexed) |
| page_size | int | 实际返回条数（通常 25，末页可能更少） |
| total | int | 阅读记录总数 |
| total_pages | int | 总页数，`total = 0` 时为 `0` |
| results | array | 当前页的最近阅读记录 |

> 元数据来自 `gallery_cache`（按 `(id, token)` 关联）：在线端点成功、加入书架预取或历史回填会把元数据写入缓存。若某条阅读记录没有对应的缓存行，`title`、`title_jpn`、`category`、`thumbnail`、`pages` 会为空字符串/零值。

**Error Responses:**

| Status Code | Description |
|-------------|-------------|
| 400 | `page < 0`，响应体 `{"error": "invalid page"}` |

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

### 20. Prefill Start（创建离线下载任务）

`POST /api/prefill`

创建后台预填充任务：服务端按 `urls` 顺序**逐页串行**抓取页面图片并写入 MinIO 缓存，不生成 ZIP，响应中不包含任何图片数据。任务由全局单 worker 严格排队执行（同一时刻只有一个任务、一次只抓一页）。进度通过轮询 `GET /api/prefill/:id` 获取。

**Request Body (JSON):**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| urls | string[] | yes | 页面 URL 列表（顺序即抓取顺序），1~2000 个，必须为 `exhentai.org` / `e-hentai.org` |
| gallery_id | int | no | 画廊 ID（与 gallery_token 一起用于去重） |
| gallery_token | string | no | 画廊 token |
| title | string | no | 任务显示标题 |

去重规则：相同 `gallery_id` + `gallery_token` 已存在 `queued`/`running` 任务时，直接返回该任务（`200`），不重复创建；否则创建新任务（`202`）。

离线下载：前端获取页面列表时会优先请求在线接口，失败或浏览器离线时回退到 `GET /api/gallery-cache/:id/:token/pages`，因此只要本地缓存过页面列表即可在 ExHentai 不可达时排队下载任务。任务按顺序重试抓取；已在 MinIO 缓存的页面直接命中，未缓存的页面抓取失败会计入 `errors`。

**示例:**

```http
POST /api/prefill
Content-Type: application/json

{
  "gallery_id": 3138775,
  "gallery_token": "30b0285f9b",
  "title": "Test Gallery",
  "urls": [
    "https://exhentai.org/s/abc/3138775-1",
    "https://exhentai.org/s/def/3138775-2"
  ]
}
```

**Response (202 创建 / 200 去重命中):**

```json
{
  "id": 1,
  "gallery_id": 3138775,
  "gallery_token": "30b0285f9b",
  "title": "Test Gallery",
  "status": "queued",
  "total": 2,
  "progress": null,
  "failed_count": 0,
  "errors": [],
  "created_at": "2026-09-28T06:00:00Z",
  "updated_at": "2026-09-28T06:00:00Z",
  "finished_at": null
}
```

**PrefillJob 对象（以下 Prefill 接口共用）:**

| Field | Type | Description |
|-------|------|-------------|
| id | int | 任务 ID |
| gallery_id | int \| null | 画廊 ID（可空） |
| gallery_token | string | 画廊 token |
| title | string | 任务标题 |
| status | string | `queued` / `running` / `completed` / `cancelled` / `failed` |
| total | int | 页面总数（= `urls` 长度） |
| progress | object \| null | 仅本进程内 `running` 时存在：`{done, cached, fetched}`。纯内存计数、不落库（图片缓存约 30 天过期，落库的进度会失真）；其余状态为 `null` |
| failed_count | int | 抓取失败的页数 |
| errors | array | 至多 20 条 `{index, url, error}` |
| created_at | string | 创建时间（RFC3339） |
| updated_at | string | 更新时间（RFC3339） |
| finished_at | string \| null | 结束时间（RFC3339），未结束为 `null` |

进程重启时，数据库中 `queued`/`running` 的任务会自动重新排队继续执行；失败页会在下载 ZIP 时按需补抓。

**Error Responses:**

| Status Code | Description |
|-------------|-------------|
| 400 | body 无效 / `urls` 为空 / 超过 2000 个 / URL 非法（域名或 scheme 不允许） |
| 503 | 数据库或缓存未配置 |

---

### 21. Prefill List（任务列表）

`GET /api/prefill`

按创建时间倒序返回最近 200 个任务。

**Response (200):**

```json
{
  "jobs": [ /* PrefillJob 数组，最新在前 */ ]
}
```

---

### 22. Prefill Get（查询单个任务）

`GET /api/prefill/:id`

返回单个 `PrefillJob` 快照，用于轮询进度。`running` 时包含 `progress`。

**Error Responses:** `400` 非法 id、`404` 不存在。

---

### 23. Prefill Cancel（取消任务）

`POST /api/prefill/:id/cancel`

取消 `queued` 或 `running` 任务。取消在响应中立即生效（状态 CAS 翻转为 `cancelled` 并写入 `finished_at`），正在抓取的 worker 随后停止。

**Response (200):** 取消后的 `PrefillJob`。

**Error Responses:**

| Status Code | Description |
|-------------|-------------|
| 400 | 非法 id |
| 404 | 不存在 |
| 409 | 任务已 `completed`/`failed`（已 `cancelled` 幂等返回 200） |

---

### 24. Prefill Delete（删除任务记录）

`DELETE /api/prefill/:id`

只删除已结束（`completed` / `cancelled` / `failed`）的任务记录。

**Response (200):**

```json
{ "deleted": 1 }
```

**Error Responses:** `409` 任务仍在排队/运行（先取消）、`404` 不存在、`400` 非法 id。

---

### 25. Prefill Cleanup（批量清理任务记录）

`POST /api/prefill/cleanup`

批量删除已结束的任务记录，进行中的任务永远不会被清理。

**Query Parameters:**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| days | int | no | `30` | 删除 N 天以前结束的记录；`0` 表示删除全部已结束记录 |

**Response (200):**

```json
{ "days": 30, "deleted": 12 }
```

**Error Responses:** `400`（`days` 为负数或非整数）、`500` 数据库错误。

---

### 26. Prefill ZIP（流式下载 ZIP）

`GET /api/prefill/:id/zip`

以 chunked 流式方式返回 `application/zip`，边读缓存边写出，不在内存中组装完整压缩包。条目按页面顺序命名为 `001.jpg` / `002.png` / …（扩展名由图片 Content-Type 决定）。

- 缓存命中：直接从 MinIO 读出写入 ZIP；
- 缓存缺失：即时抓取补全（经 singleflight 去重，不排队在 worker 后面）；
- 缺失且抓取失败的页会被跳过（ZIP 中少一个条目）。

**Headers:**

```http
Content-Type: application/zip
Content-Disposition: attachment; filename="..."; filename*=UTF-8''...
Cache-Control: no-store
```

**HEAD `/api/prefill/:id/zip`:** 探测 ZIP 是否就绪，不返回正文（前端在下载前调用）。状态码与 GET 相同。

**Error Responses:** `404` 不存在、`400` 非法 id、`503` 数据库或缓存未配置。

---

## 离线缓存（gallery-cache）

在线端点（`/api/gallery/:id/:token`、`/details`、`/pages`）只负责访问上游；**完整成功**时会把结果写入 `gallery_cache`（`/pages` 只有整份列表抓取成功才写入，失败绝不写入部分数据），但**不做缓存回退**。回退与重试全部由前端编排：先请求在线端点（带重试），失败或浏览器离线时再请求下面的只读缓存端点；`/pages` 的失败包括流中途以 `error` 行终止。

### Cached Gallery

`GET /api/gallery-cache/:id/:token`

只读 `gallery_cache` 中的元数据快照，响应结构与 `/api/gallery/:id/:token` 相同。

### Cached Gallery Details

`GET /api/gallery-cache/:id/:token/details`

只读缓存的详情快照，响应结构与 `/api/gallery/:id/:token/details` 相同；`domain`/`parent`/`visible` 等未缓存字段返回零值。

### Cached Gallery Pages

`GET /api/gallery-cache/:id/:token/pages`

返回缓存的页面列表（**普通 JSON，非流式**）：`{ "id", "token", "total", "pages": [...] }`，`pages` 元素结构与在线 `/pages` 流中的 `page` 行一致。

**说明：**

- 不访问上游、不重试；无缓存时返回 `404`
- 响应头 `Cache-Control: no-store`（反向代理不应缓存）

### 缓存写入时机

- 在线端点完整成功时（元数据 / 详情 / 页面列表；`/pages` 抓取不完整时**不写入**）
- `POST /api/bookshelf/:id/:token` 成功后后台异步预取元数据与页面列表（页面列表同样仅在完整成功时写入）

缓存采用「首次写入优先」策略：已存在的非空字段不会被覆盖（上游元数据变动不频繁）。

图片本身由 `/api/image-cache/*` 图片代理提供（MinIO 缓存，见第 9-11 节），离线可读的前提是对应页面图片此前已被浏览或下载过。

清理：当某条缓存既不在书架也不在阅读记录中时，会随「书架移除」「清理阅读记录」以及服务启动时被删除。

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
| 409 | Conflict (任务状态不允许该操作) |
| 500 | Internal server error |
| 502 | Upstream error (ExHentai API 或抓取失败) |
| 503 | Service unavailable (依赖未配置) |
