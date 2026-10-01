# Manga Reader Frontend

ExHentai Web 漫画客户端的前端。

单用户、自托管、Docker 部署。本阶段实现 5 个核心页面（首页 / 订阅 / 热门 / 搜索 / 书架）及基础架构。

## 技术栈

- Bun
- TypeScript
- React
- Vite
- Cloudflare Kumo
- React Router
- TanStack Query

## 开发

```bash
bun install
bun run dev
```

前端通过 `VITE_API_BASE_URL` 直连后端 API（默认 `http://localhost:8080`，后端需允许 CORS）。

### EhTagTranslation 翻译数据库

`public/db.text.js` 是内置的 Tag 中文翻译数据库（首次访问兜底）。日常通过右上角更新按钮从 GitHub 拉取最新版（存入 IndexedDB）。如需刷新内置种子文件：

```bash
bun run db:update
```

## 环境变量

复制 `.env.example` 为 `.env` 并按需修改：

```env
VITE_API_BASE_URL=http://localhost:8080
```

## 构建

```bash
bun run build
```

## 部署（nginx，方案 A：托管前端 + 反代 /api）

1. 构建生产产物（读取 `.env.production`，`VITE_API_BASE_URL` 为空 → 前端走同源 `/api/*`）：

   ```bash
   bun run build
   ```

2. 将 `dist/` 复制到服务器，如 `/var/www/manga-reader/dist`。

3. 使用 `deploy/nginx.conf`（按需改 `server_name`、`root`、后端 `proxy_pass` 地址），启用并重载：

   ```bash
   sudo cp deploy/nginx.conf /etc/nginx/sites-available/manga-reader
   sudo ln -s /etc/nginx/sites-available/manga-reader /etc/nginx/sites-enabled/
   sudo nginx -t && sudo systemctl reload nginx
   ```

说明：

- 前端所有 `/api/*`（含 `cached-image`/`cached-thumbnail`）由 nginx 转发到后端，无需后端 CORS。
- SPA 路由靠 `try_files ... /index.html` 回退。
- `db.text.js`（翻译库）同源加载，更新按钮仍从 GitHub 拉取。

## 列表渲染

首页 / 订阅 / 热门 / 搜索 / 书架 / 最近阅读等列表页统一使用：

- **响应式瀑布流网格**：`.gallery-grid` 按视口宽度自动切换 2 / 3 / 4 / 5 / 6 / 7 列（断点 480 / 768 / 1024 / 1440 / 1920px），卡片封面固定 `3:4`。
- **无限滚动**：滚动接近底部时自动请求下一页并追加，已移除上一页 / 下一页按钮。
- **懒加载**：缩略图使用 `loading="lazy"` + `decoding="async"`，数据按 `page` 分页按需加载。
- **提前加载**：`useInfiniteScroll` 通过 `IntersectionObserver`（`rootMargin: 0px 0px 200% 0px`，约提前 2 屏）预取下一页，加载过程不显示动画。
- **Jump/Seek**：首页 / 订阅 / 搜索页提供 `JumpSeekBar`（文本框自动识别日期 `seek` 或偏移 `jump`，附 `1d/3d/1w/1m/1y` 快捷按钮），配合后端返回的 `nav` 元数据定位到指定日期或相对位置。首页 / 订阅使用局部 state，搜索页写入 URL 查询参数（`seek` / `jump`）。

阅读器（comimi）的页面列表会为每一页创建缩略图，大画廊（上千页）会瞬间发起大量 `/api/image-cache/page-thumbnail` 请求。`observeLazyThumbnails` 拦截这些图片，只加载进入视口附近的部分，并通过并发上限为 6 的队列逐个请求。

相关实现：`src/hooks/useInfiniteScroll.ts`、`src/components/common/InfiniteScrollTrigger.tsx`、`src/components/common/JumpSeekBar.tsx`、`src/lib/jumpSeek.ts`、`src/lib/lazyThumbnails.ts`，以及 `src/hooks/useGalleryList.ts`、`useSearch.ts`、`useBookshelf.ts`、`useRecentlyRead.ts`（均为 `useInfiniteQuery`）。

## 路由

| 路径 | 页面 |
|------|------|
| `/` | 首页 |
| `/watched` | 订阅 |
| `/popular` | 热门 |
| `/search` | 搜索 |
| `/bookshelf` | 书架 |
| `/recently-read` | 最近阅读 |
| `/gallery/:id/:token` | Gallery Detail |
| `/reader/:id/:token` | Reader |
