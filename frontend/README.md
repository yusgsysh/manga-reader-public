# Manga Reader Frontend

ExHentai Web 漫画客户端的前端。

单用户、自托管、Docker 部署。页面：首页 / 订阅 / 热门 / 搜索 / 书架 / 最近阅读 / 下载管理 / 设置 / Gallery 详情 / 阅读器。

## 技术栈

- Bun
- TypeScript
- React
- Vite
- Tailwind CSS
- React Router
- TanStack Query
- comimi / comimi-react（阅读器，本地补丁，见「comimi / comimi-react 补丁」）
- Cloudflare Kumo
- oxlint / vitest / Playwright

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

## comimi / comimi-react 补丁

本仓库对 `@yui540/comimi` 维护三处补丁，均由同一个 `patches/@yui540%2Fcomimi@0.26.0.patch` 承载：**预加载页白屏**、**触屏进度条缩略图**与**阅读器返回/全屏入口迁入 comimi（并移除宽屏选项）**。另有 `patches/@yui540%2Fcomimi-react@0.3.0.patch`，把新增事件加入运行时事件白名单，使 `onBack` / `onFullscreenRequest` 能作为 props 透传。

### 补丁一：预加载页白屏

#### 背景

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

### 补丁二：触屏进度条缩略图

#### 背景

桌面上拖动底部进度条会显示页面缩略图预览，但触屏设备上不显示。

根因有两处：`@yui540/comimi@0.26.0` 的 `ControlsDock.buildSeek`（`dist/index.js`，约 1583 行）只给 `.comimi-seek-bar` 绑定了 `mousemove` / `mouseleave`，没有触摸事件；同时其内置样式对 `.comimi-seek-preview` 有 `@media (hover: none) { display: none; }`，主动在触屏设备上隐藏预览。

#### 补丁

`patches/@yui540%2Fcomimi@0.26.0.patch` 追加触摸监听，并让 `updateSeekPreview` 在 `clientX` 缺失时从 `touch` 事件取坐标：

```diff
 		}), this.seekBar.addEventListener("mousemove", (e) => this.updateSeekPreview(e)), this.seekBar.addEventListener("mouseleave", () => {
 			this.seekPreview.dataset.show = "false";
+		}), this.seekBar.addEventListener("touchstart", (e) => this.updateSeekPreview(e), { passive: !0 }), this.seekBar.addEventListener("touchmove", (e) => this.updateSeekPreview(e), { passive: !0 }), this.seekBar.addEventListener("touchend", () => {
+			this.seekPreview.dataset.show = "false";
+		}), this.seekBar.addEventListener("touchcancel", () => {
+			this.seekPreview.dataset.show = "false";
 		}), this.seekBar.append(n, this.seekInput, this.seekPreview), e.append(t, this.seekBar), e;
 	}
 	updateSeekPreview(e) {
@@
 		let r = this.seekBar.getBoundingClientRect();
 		if (r.width === 0) return;
-		let i = Math.max(0, Math.min(r.width, e.clientX - r.left)), ...
+		let d = e.clientX;
+		if (d === void 0) {
+			let f = e.touches && e.touches[0] || e.changedTouches && e.changedTouches[0];
+			if (!f) return;
+			d = f.clientX;
+		}
+		let i = Math.max(0, Math.min(r.width, d - r.left)), ...
```

触摸监听用 `{ passive: true }`，不阻止默认行为，滑动 `<input type="range">` 仍照常翻页。

内置样式在触屏上隐藏预览，需在 `src/index.css` 用更高优先级的规则解除（`0,2,0` > `0,1,0`，与注入顺序无关）：

```css
@media (hover: none) {
  .reader-shell .comimi-seek-preview {
    display: flex;
  }
}
```

### 补丁三：阅读器返回/全屏入口迁入 comimi（移除宽屏）

#### 背景

阅读器原本自绘一条 `.reader-topbar`：返回、页码、离线徽标、全屏切换。进入 comimi 的全屏布局后，库把 `.comimi-root` 设为 `position: fixed; inset: 0`，会盖住顶栏，返回与退出全屏都变得不可达。现在把返回与全屏入口收进 comimi 自身 UI，并移除顶部栏（页码与「离线数据」徽标一并移除）。

#### comimi 侧补丁

- **事件**：`dist/types.d.ts` 的 `ViewerEventMap` 新增 `back` / `fullscreenRequest`；`dist/index.js` 的核心回调新增 `requestBack` / `requestFullscreen` 并分别 `emit`。`requestFullscreen` 在没有监听者时回退到 `setLayoutMode("browserFullscreen")`。
- **返回入口**：`buildMenuView` 在「关于 comimi」下方追加 `menu.backToGallery` 条目，点击关闭菜单并派发 `back`。
- **全屏入口**：dock 视图切换器的全屏按钮与 `F` 键改为派发 `fullscreenRequest`，由应用侧统一处理（保持原来的 `browserFullscreen` + 原生 `requestFullscreen` + `fullscreenchange` 同步）。
- **移除宽屏**：视图切换器 `entries` 去掉 `wide`；内置 CSS `repeat(3, 42px)` / `126px` 改为 `repeat(2, 42px)` / `84px`；快捷键列表去掉 `W`；键盘 `W` 分支删除。

```diff
+	has(e) {
+		let t = this.handlers.get(e);
+		return !!t && t.size > 0;
+	}
 	emit(e, t) {
@@
 		reportPageLoadError: (e) => {
 			let t = this.store.getState().manga.pages[e];
 			t && this.events.emit("pageLoadError", {
 				pageIndex: e,
 				page: t
 			});
+		},
+		requestBack: () => {
+			this.events.emit("back", void 0);
+		},
+		requestFullscreen: () => {
+			this.events.has("fullscreenRequest") ? this.events.emit("fullscreenRequest", void 0) : this.setLayoutMode("browserFullscreen");
 		}
 	};
```

`menu.backToGallery` 的文案不放进库内 locale，而是由 `ReaderPage` 通过 `translations` 传入（见下）。

#### comimi-react 侧补丁

`dist/index.js` 的事件白名单 `V` 增加 `"back"` / `"fullscreenRequest"`。comimi-react 的 `on<EventName>` props 类型由 comimi 的 `ViewerEventMap` 推导，运行时却由这份硬编码白名单决定，因此两处必须同时加上，`onBack` / `onFullscreenRequest` 才会真正生效；它也会把这两个 prop 从透传到 DOM 的 rest props 中剔除。

#### 应用侧

`ReaderPage` 删除 `.reader-topbar`，改用 `<MangaViewer onBack={...} onFullscreenRequest={...} />`：`onBack` 执行 `flushProgress()` + `goBack()`（沿用 `useBackNavigation` 的浏览器历史 / 深链回退规则）；`settings.layoutMode` 默认 `inline`（标准），用户可点 dock 的全屏按钮进入全屏。

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

   触屏进度条缩略图没有自动化回归，需在触屏设备或 DevTools 触摸模拟下手动确认；若上游已支持触摸事件并移除了 `@media (hover: none)` 隐藏，可一并去掉 `src/index.css` 里的 `.reader-shell .comimi-seek-preview` 覆盖。

2. 重新打补丁（comimi）：

   ```bash
   bun patch @yui540/comimi@x.y.z
   # 按「补丁一 / 补丁二 / 补丁三」的 diff 修改
   # node_modules/@yui540/comimi/dist/index.js 与 dist/types.d.ts
   bun patch --commit 'node_modules/@yui540/comimi'
   ```

   bun 会生成新的 `patches/@yui540%2Fcomimi@x.y.z.patch` 并更新 `patchedDependencies`；确认无误后删除旧补丁文件。

3. 若 `@yui540/comimi-react` 也升级，重新打事件白名单补丁：

   ```bash
   bun patch @yui540/comimi-react@x.y.z
   # 确认 node_modules/@yui540/comimi-react/dist/index.js 的事件白名单 V 中
   # 仍包含 "back" / "fullscreenRequest"
   bun patch --commit 'node_modules/@yui540/comimi-react'
   ```

4. 验证：`bun run lint && bun run build && bun run test && bun run test:e2e`。

5. 建议同时向上游提 issue/PR（合并两条渲染分支、补齐触摸事件、暴露 back/全屏事件与隐藏宽屏选项），最终移除补丁。

## 环境变量

复制 `.env.example` 为 `.env` 并按需修改：

```env
VITE_API_BASE_URL=http://localhost:8080
```

## 构建

```bash
bun run build
```

## 部署（内嵌进后端单镜像）

前端没有独立镜像：根 `Dockerfile` 先用 Bun 构建生产产物（`VITE_API_BASE_URL` 强制留空 → 前端走同源 `/api/*`），再把 `dist/` 拷进 `backend/internal/web/dist`，由 `//go:embed` 编译进 Go 二进制，Gin 同时服务静态文件与 `/api/*`（SPA 回退、缓存策略见 `backend/internal/web`）。

```bash
# 仓库根目录
docker build -t manga-reader .
docker run -d -p 5173:8080 --env-file .env manga-reader
```

- 根 `docker-compose.yml` 把该服务发布到 `5173:8080`；镜像由 CI 构建推送（见根 `README.md` 的「CI 与部署」）。
- 本地 `go run .` 想同时看到页面时，先 `bun run build` 并把 `dist/` 拷到 `backend/internal/web/dist`（否则后端以 API-only 模式启动）。

纯静态部署（自行用 nginx / caddy 托管并反代 `/api`）也只需 `bun run build` 的 `dist/`：

- 前端所有 `/api/*` 转发到后端，无需后端 CORS；
- SPA 路由靠 `try_files ... /index.html` 回退；
- `db.text.js`（翻译库）同源加载，更新按钮仍从 GitHub 拉取。

## 缓存与 staleTime

后端 `gallery_cache` **无 TTL、read-through**（详见根 `README.md` 的「缓存模型」）：在线端点始终回源、不写缓存；缓存端点在入口命中时由前端优先渲染，未命中时后端流式回源并回填。此外，后端对同一上游页面/元数据请求做 **10 秒进程内 singleflight 去重**（`backend/internal/exhentai/fetchcache.go`），所以详情、页列表、总数等从同一页面派生的数据只抓一次。图片加载失败时前端**直接失败、不自动重试**，失败已触发后端后台刷新，重新打开页面即可拿到新列表。页面缩略图（`/api/image-cache/page-thumbnail` 按索引寻址）的精灵图过期是例外：后端会在该次请求内刷新并重试，前端首个请求即可拿到图。

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
- **Jump/Seek**：首页 / 订阅 / 搜索页提供 Jump/Seek 面板（`JumpSeekPanel`：文本框自动识别日期 `seek` 或偏移 `jump`，附 `1d/3d/1w/1m/1y` 快捷按钮；首页 / 订阅通过 `JumpSeekMenu` 从页头弹出，搜索页直接内嵌），配合后端返回的 `nav` 元数据定位到指定日期或相对位置。首页 / 订阅使用局部 state，搜索页写入 URL 查询参数（`seek` / `jump`）。

阅读器（comimi）的页面列表会为每一页创建缩略图，大画廊（上千页）会瞬间发起大量 `/api/image-cache/page-thumbnail` 请求。`loadThumbnails` 拦截这些图片，**不做视口懒加载**：缩略图出现即入队加载，通过并发上限为 5 的队列逐个请求；节点被移除时会取消占位并释放槽位。阅读器会给 `loadThumbnails` 传 `getLimit`，只预取已从 `/pages` 流式收到的页（`index < 已收页数`），新页到达时调用返回句柄的 `refresh()` 补入队，因此预取不会超过 `pages` 的数量。

阅读器用 detail 的 `page_count` 作为权威总页数，从第一帧起就给 comimi 一份固定长度（`total`）的页面槽位；真实页面 URL 由 `resolvePageSrc` 随流式数据懒解析，因此总页数、页面列表与缩略图不会随 `pages` 更新而刷新（`galleryPageSlotsToManga` / `PageUrlStore` / `slotPageSrcResolver`）。

Gallery 详情页也提供页面缩略图网格（`PageThumbnailGrid`，每页 20 张、`SimplePagination` 翻页），点击某页跳转到 `/reader/:id/:token?page=N`；阅读器把 `?page`（0-indexed）作为初始页，离开时把进度保存为该页。

相关实现：`src/hooks/useInfiniteScroll.ts`、`src/components/common/InfiniteScrollTrigger.tsx`、`src/components/common/JumpSeekPanel.tsx` / `JumpSeekMenu.tsx`、`src/lib/jumpSeek.ts`、`src/lib/thumbnails.ts`，以及 `src/hooks/useGalleryList.ts`、`useSearch.ts`、`useBookshelf.ts`、`useRecentlyRead.ts`（均为 `useInfiniteQuery`）。

## 路由

| 路径 | 页面 |
|------|------|
| `/` | 首页 |
| `/watched` | 订阅 |
| `/popular` | 热门 |
| `/search` | 搜索 |
| `/bookshelf` | 书架 |
| `/recently-read` | 最近阅读 |
| `/downloads` | 下载管理 |
| `/settings` | 设置 |
| `/gallery/:id/:token` | Gallery Detail |
| `/reader/:id/:token` | Reader |

列表页通过 `NavigationContext` + `useLastListRoute` 记住来源路由：进入 Gallery 详情时侧边栏 / 抽屉高亮来源列表项，直接访问（无 referrer）时回落到首页。
