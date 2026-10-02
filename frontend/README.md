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

## 测试

```bash
bun run test   # 单元测试（vitest）
bun run lint   # oxlint
```

阅读器页面加载的端到端测试（Playwright）不需要真实后端：`e2e/mock-server.mjs` 会托管生产构建并 mock `/api/*`。

```bash
bun run test:e2e:install   # 首次安装 Chromium
bun run test:e2e           # 以 e2e 模式构建（同源 API）并运行
```

覆盖两种关键行为：已有完整缓存的画廊从缓存即时打开（完全不请求在线 `/pages`）；无缓存时渐进流式，首批到达即挂载、无需等待整份列表。

## comimi 补丁（@yui540/comimi）

### 背景

阅读器翻到「已预加载」的页面时，如果图片还没加载出来，会短暂白屏，而不是显示 comimi 的兔子加载动画。

根因在 `@yui540/comimi@0.26.0` 的 `PageStage.buildSlot`（`dist/index.js`，约 1809 行）：它有一条快速分支，当 `imageSources` 已缓存该页的解析结果时，直接 `img.src = url` 返回，既不插入 `.comimi-loading-icon`（兔子），也不把 `<img>` 设为 `visibility: hidden`。而 `PageStage.preloadImages()` 会预热当前页 ±4 页，所以翻到这些页就命中快速分支，解码前只露出白底。

### 补丁

`patches/@yui540%2Fcomimi@0.26.0.patch` 把该快速分支改成与正常分支一致（插入兔子 + 隐藏图片，`load` 后移除图标）。`package.json` 的 `patchedDependencies` 声明它，`bun install` 会自动应用。

升级后重新打补丁时，照下面的 diff 修改 `node_modules/@yui540/comimi/dist/index.js` 里 `buildSlot` 的快速分支：

```diff
 		let o = `${e.manga.id}:${t.id}`, s = this.imageSources.get(o);
-		if (s) return a.src = s, {
-			slot: i,
-			img: a
-		};
+		if (s) {
+			let c = Ie(this.options.i18n, this.options.loadingMascot);
+			i.append(c), a.style.visibility = "hidden", a.addEventListener("load", () => {
+				a.style.visibility = "", c.remove();
+			}, { once: !0 }), a.src = s;
+			return {
+				slot: i,
+				img: a
+			};
+		}
 		let c = Ie(this.options.i18n, this.options.loadingMascot);
```

注意：`dist/manga-viewer.global.js` 是浏览器全局构建，Vite 不引用，无需修改。

### 回归测试

`e2e/repro-reader-loading.mjs` 延迟整页图片，先验证首页有兔子，再跳到预加载页采样。

```bash
bun run test:e2e                  # 含回归门禁（reader-pages + 兔子加载）
bun run test:e2e:reader-loading   # 仅回归门禁（需先 build:e2e）
bun run repro:reader-loading      # 诊断探针：仍白屏则 exit 0，已修复则 exit 1
```

### 升级 comimi 时如何重新打补丁

1. 升级版本（如 `bun add @yui540/comimi@x.y.z`）后，先检查上游是否已修复：

   ```bash
   bun run build:e2e
   EXPECT_FIXED=1 bun e2e/repro-reader-loading.mjs
   ```

   - 通过（兔子正常）：上游已修，删除 `patches/@yui540%2Fcomimi@*.patch` 和 `package.json` 的 `patchedDependencies`，本段可只留作历史记录。
   - 失败：继续下一步。

2. 重新打补丁：

   ```bash
   bun patch @yui540/comimi@x.y.z
   # 按上面的 diff 修改 node_modules/@yui540/comimi/dist/index.js
   bun patch --commit 'node_modules/@yui540/comimi'
   ```

   bun 会生成新的 `patches/@yui540%2Fcomimi@x.y.z.patch` 并更新 `patchedDependencies`；确认无误后删除旧补丁文件。

3. 验证：`bun run test:e2e`。

4. 建议同时向上游提 issue/PR，把两条渲染分支合并，最终移除补丁。

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

## 缓存与 staleTime

后端 `gallery_cache` **无 TTL、read-through**（详见根 `README.md` 的「缓存模型」）：在线端点始终回源、不写缓存；缓存端点在入口命中时由前端优先渲染，未命中时后端流式回源并回填。此外，后端对同一上游页面/元数据请求做 **10 秒进程内 singleflight 去重**（`backend/internal/exhentai/fetchcache.go`），所以详情、页列表、总数等从同一页面派生的数据只抓一次。图片加载失败时前端**直接失败、不自动重试**，失败已触发后端后台刷新，重新打开页面即可拿到新列表。

react-query 的 `staleTime` 仅用于客户端节流，统一集中在 `src/lib/cacheConfig.ts`：

| 场景 | 值 |
|------|----|
| 全局默认 | 30s |
| pages | 10m |
| gallery / gallery-detail / search / download-pages | 5m |
| bookshelf / recently-read / gallery-list | 2m |
| progress / prefill | 30s |
| settings | 0 |

## 列表渲染

首页 / 订阅 / 热门 / 搜索 / 书架 / 最近阅读等列表页统一使用：

- **响应式瀑布流网格**：`.gallery-grid` 按视口宽度自动切换 2 / 3 / 4 / 5 / 6 / 7 列（断点 480 / 768 / 1024 / 1440 / 1920px），卡片封面固定 `3:4`。
- **无限滚动**：滚动接近底部时自动请求下一页并追加，已移除上一页 / 下一页按钮。
- **懒加载**：缩略图使用 `loading="lazy"` + `decoding="async"`，数据按 `page` 分页按需加载。
- **提前加载**：`useInfiniteScroll` 通过 `IntersectionObserver`（`rootMargin: 0px 0px 200% 0px`，约提前 2 屏）预取下一页，加载过程不显示动画。
- **Jump/Seek**：首页 / 订阅 / 搜索页提供 `JumpSeekBar`（文本框自动识别日期 `seek` 或偏移 `jump`，附 `1d/3d/1w/1m/1y` 快捷按钮），配合后端返回的 `nav` 元数据定位到指定日期或相对位置。首页 / 订阅使用局部 state，搜索页写入 URL 查询参数（`seek` / `jump`）。

阅读器（comimi）的页面列表会为每一页创建缩略图，大画廊（上千页）会瞬间发起大量 `/api/image-cache/page-thumbnail` 请求。`loadThumbnails` 拦截这些图片，**不做视口懒加载**：缩略图出现即入队加载，通过并发上限为 5 的队列逐个请求；节点被移除时会取消占位并释放槽位。阅读器会给 `loadThumbnails` 传 `getLimit`，只预取已从 `/pages` 流式收到的页（`index < 已收页数`），新页到达时调用返回句柄的 `refresh()` 补入队，因此预取不会超过 `pages` 的数量。

阅读器用 detail 的 `page_count` 作为权威总页数，从第一帧起就给 comimi 一份固定长度（`total`）的页面槽位；真实页面 URL 由 `resolvePageSrc` 随流式数据懒解析，因此总页数、页面列表与缩略图不会随 `pages` 更新而刷新（`galleryPageSlotsToManga` / `PageUrlStore` / `slotPageSrcResolver`）。

Gallery 详情页也提供页面缩略图网格（`PageThumbnailGrid`，每页 20 张、`SimplePagination` 翻页），点击某页跳转到 `/reader/:id/:token?page=N`；阅读器把 `?page`（0-indexed）作为初始页，离开时把进度保存为该页。

相关实现：`src/hooks/useInfiniteScroll.ts`、`src/components/common/InfiniteScrollTrigger.tsx`、`src/components/common/JumpSeekBar.tsx`、`src/lib/jumpSeek.ts`、`src/lib/thumbnails.ts`，以及 `src/hooks/useGalleryList.ts`、`useSearch.ts`、`useBookshelf.ts`、`useRecentlyRead.ts`（均为 `useInfiniteQuery`）。

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
