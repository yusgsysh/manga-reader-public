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
| q | string | yes | - | Search keyword |
| site | string | no | `"exhentai"` | `"exhentai"` or `"ehentai"` |
| categories | string | no | - | Comma-separated categories, e.g. `"doujinshi,manga"` |
| page | int | no | `0` | Page number (0-indexed) |

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

Proxies the image from ExHentai. Returns raw image bytes with the original `Content-Type`.

**Query Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| url | string | yes | Page URL (from `page_urls` or `pages` array) |

**Response:**

- Success: Raw image data (`Content-Type: image/jpeg` or `image/png`)
- NL fallback: Automatically retries up to 2 times if initial download fails

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
| page | int | no | `0` | Page number (0-indexed) |

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

Same response format as `/api/gallerys`.

---

### 8. Popular List

`GET /api/popular`

Same response format as `/api/gallerys`.

---

## Error Responses

All error responses follow this format:

```json
{
  "error": "error message"
}
```

| Status Code | Description |
|-------------|-------------|
| 400 | Bad request (invalid parameters) |
| 502 | Upstream error (ExHentai API or scrape failed) |
