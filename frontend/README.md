# Manga Reader Frontend

ExHentai Web 漫画客户端的前端。

多用户、自托管、Docker 部署。页面：登录 / 首页 / 订阅 / 热门 / 搜索 / 书架 / 最近阅读 / 下载管理 / 设置 / Gallery 详情 / 阅读器。

## 登录与认证

后端始终启用多用户 JWT。未登录访问受保护页面时会跳转至 `/login`；登录表单只需要用户名和密码。登录成功后 JWT 由后端写入 `HttpOnly; SameSite=Lax` Cookie（前端只保留内存副本用于同标签页的 `Authorization` 头），登录响应同时带回当前用户，刷新页面则通过 `/api/auth/status` 恢复用户和角色。任何 API 返回 `401` 都会统一清理认证状态并跳回登录页。

「设置」页集中了个人设置（外观、ExHentai Cookie、标签规则、标签翻译数据库、数据同步）与管理员功能（用户管理、API Key）；管理员登录后才会看到「用户管理」与「API Key」两个区块，后端也会独立校验角色。每个用户的 ExHentai Cookie 与个人设置在「设置」页独立维护。

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

本仓库对 `@yui540/comimi` 维护四处补丁，均由同一个 `patches/@yui540%2Fcomimi@0.26.0.patch` 承载：**预加载页白屏**、**触屏进度条缩略图**、**阅读器返回/全屏入口迁入 comimi（并移除宽屏选项）**与**翻页动画（平移/书本）**。另有 `patches/@yui540%2Fcomimi-react@0.3.0.patch`，把新增事件加入运行时事件白名单，使 `onBack` / `onFullscreenRequest` 能作为 props 透传。

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

### 补丁四：翻页动画（平移 / 书本）

#### 背景

comimi 原生的翻页只有一种整块舞台的横向平移，而且只在触摸滑动结束时的「惯性翻页」路径触发；点击屏幕左右两侧、左右箭头按钮和键盘翻页都是即时跳转、没有任何过渡。要让阅读器支持「平移」和「书本」两种可切换的翻页动画，需要在 comimi 里新增一个设置项，并把所有单步翻页路径统一收口到动画入口。

书本翻页最初由 comimi 补丁用 CSS `rotateY` 实现，之后又在应用侧把 grabfold 的 `FoldingSheet` 叠加到 comimi 舞台上。这两种做法都只是在 comimi 的单页舞台外面套一层动画：页面被克隆、背面留空、无法做双页映射，也没有 WebGL 卷曲。现在「书本」是一个独立的阅读器实现（`BookFlipViewer`），使用 grabfold 官方的 React 适配器与 WebGL 渲染器；comimi 只负责「平移」等其余模式。

#### comimi 侧补丁

- **设置项**：`dist/types.d.ts` 新增 `PageTurnAnimation = "slide" | "book"`，`ViewerSettings` 增加 `pageTurnAnimation` 字段；`dist/index.js` 的默认值对象补 `pageTurnAnimation: "slide"`。
- **渲染器状态**：`ViewerRenderer.update` 把 `pageTurnAnimation` 写到根节点的 `data-page-turn-animation` 属性，供 CSS 切换 `perspective` / 过渡。
- **动画入口统一**：核心 `nextPage` / `previousPage` 原来直接 `commitNextPage` / `commitPreviousPage`（点击、箭头、键盘都走这里），改为先走 `renderer.requestPageTurn(state, direction)`（即 `animatePageTurn`），仅在无法动画（已是边界、正在动画中）时回退到直接提交；动画期间 `isPageTurnAnimating` 挡住重复触发。
- **书本实现**：`PageStage` 新增 `beginBookFlip(direction, settings)` / `endBookFlip()`。`animatePageTurn` 在 `pageTurnAnimation === "book"` 且未放大（`zoomScale <= 1`，放大时页面组带内联 transform，不能叠加旋转）时走书本，否则沿用原来的整块平移。
  - `beginBookFlip` 先把舞台复位（`snapTransform`），给「当前页」组打上 `data-spine`（根据阅读方向与翻页方向算出书脊在左还是右），把「目标页」组的 transform 改为 `translateX(0%)`（靠 CSS 过渡滑入），并在舞台上标记 `data-flipping`。
  - 180ms 后 `endBookFlip` 清除标记，再由回调提交页面（提交是同步的，会立刻重建舞台，因此不会闪一帧「翻回来的过渡」）。
- **CSS**：在 `.comimi-stage` 的样式串尾部追加书本翻页基础样式——舞台 `perspective`、书脊侧阴影等。
- **现状**：应用在「书本」模式下不再挂载 comimi，因此补丁里的 `beginBookFlip` / 书本 CSS 不会被触发；`pageTurnAnimation` 设置本身仍由补丁提供类型与默认值，应用用它来选择 `BookFlipViewer`。保留补丁的这部分实现，是为了不破坏 comimi 自身的其它宿主用法。

#### 应用侧

`ReaderPage` 的 `settings` 增加 `pageTurnAnimation: "slide"` 默认值；`ReaderToolbar` 的「阅读设置」面板新增「翻页动画」分段控件（平移 / 书本），点击调用 `updateSettings({ pageTurnAnimation })`。该偏好与其他阅读设置一样随会话有效、不持久化（comimi 的 `storage` 已关闭）。

#### GrabFold 书本阅读器

「书本」模式现在由 [grabfold](https://github.com/javocsoft/grabfold)（[npm](https://www.npmjs.com/package/grabfold)，MIT License © JavocSoft）驱动，封装为独立的 `BookFlipViewer`：

- **按模式分发**：`ReaderPage` 根据 `pageTurnAnimation` 在 `MangaViewer`（平移）与 `BookFlipViewer`（书本）之间切换。`BookFlipViewer` 通过 `React.lazy` 动态加载（构建产物里是独立的 `BookFlipViewer-*.js` chunk），平移模式不会加载 grabfold 的运行时。
- **使用官方适配器**：`grabfold/react` 的 `GrabfoldBook` 负责书本、翻页、拖动、flick、peek、整叠抓页与生命周期；`grabfold/webgl` 的 `webglFold` 把翻起的纸张映射到圆柱面并在 GPU 上渲染折痕、卷曲、透视与动态光照；DOM 折叠（grabfold 默认渲染器）是自动回退方案。
- **页面映射**（`src/lib/bookPages.ts`）：漫画页始终是 0-based，grabfold 的 position 只由漫画页推导，从不写入后端。
  - 双页：`Sheet n` 的正面是第 `2n` 页、背面是第 `2n+1` 页；显示封面时第 0 页单独占右侧首屏，之后成对 `(1,2)`、`(3,4)`…；隐藏封面时首屏就是 `(0,1)`（Sheet 0 的正面是空白的环衬）。
  - 单页：单页布局只显示纸张正面，所以每张 Sheet 只承载一张漫画页（`Sheet n` 正面 = 第 `n` 页）；翻页时纸张背面显示同一张图的镜像作为纸背图案，不再是空白灰纸。背面由折页的翻折变换自然镜像。
  - 布局或封面开关变化时，按当前漫画页重新计算 Sheet 数量与 position，并重建书本，因此切换前后停留的是同一张漫画页。
- **阅读方向**：`binding` 由阅读方向决定（RTL → `"right"`，LTR → `"left"`）；页面内容不做镜像。方向切换只改变装订与翻页方向，position 保持不变。
- **交互**：拖动、触摸、flick、peek、整叠抓页由 grabfold 直接处理；点击左右 30% 区域、方向键、PageUp/PageDown、Home/End、空格、`A`/`F`/`O`/`P`/`C` 与底部控制条（上一页 / 页码输入 / 进度条 / 下一页）都通过同一个组件句柄驱动，工具栏与页码列表也走 `goToPage`，不存在第二套页码状态。底部控制条由 `ReaderControlsBar` 提供，平移与书本模式共用同一组控件；comimi 自己的 dock 与左右箭头在阅读器内隐藏，避免两套控件并存。控制条的显示/隐藏沿用 comimi 的规则（出现 3 秒后自动隐藏，点击中间区域切换），顶部工具栏随浮层一起显示。自动播放时，书本模式在控制条隐藏后显示与 comimi 相同的底部灰色进度线（`ReaderAutoplayProgress`，动画时长跟随自动翻页间隔）。
- **图片与纹理**（`src/lib/pageImages.ts`）：页面 URL 仍由现有的 `/api/image-cache/page` 接口解析（`PageUrlStore` 流式注入），每张图只解析、解码一次；同一份 `HTMLImageElement` 既交给 WebGL 纹理，也用于页面 `<img>`（同一 URL，浏览器缓存复用）。缓存按字节数和张数封顶（桌面 112MB / 10 张，移动端 48MB / 6 张），预加载/保留当前页前后各一到两个跨页，翻页前已解码；离开视窗的页面被 LRU 淘汰。单页解码失败时页内显示「重试」。
- **生命周期**：grabfold 实例与 `GrabfoldBook` 组件绑定，卸载时 `destroy()` 销毁 WebGL 上下文与纹理；WebGL 上下文只在书本视图存活期间存在，切换模式即释放。StrictMode 的双次挂载/卸载已验证只保留一个实例、一个 canvas。`prefers-reduced-motion` 交给 grabfold 处理（瞬时翻页）。
- **WebGL 回退**：先用一次性探测 canvas 检查 `webgl2`/`webgl`（探测后立即 `loseContext`），不可用时不创建 `webglFold`；即使上下文创建或着色器链接失败，`webglFold` 自身也会退回 DOM 折叠，阅读器不会因此不可用。
- **阅读进度**：只在翻页落定（`onPositionChange`）时把锚点漫画页交给 `useReadingProgressSync` 的 debounce 保存；拖动中途松手回弹不会写入；退出阅读器时照常 flush。重新进入时可从后端进度恢复到同一页。
- **视觉参数**：`pageRatio` 取第一张成功加载的漫画页比例（避免把跨页大图当成书本比例）；`duration=620`、`commitAt=0.38`、`dragThreshold=8`、`thickness=14`、`shadow=0.32`、`gutter=0.24`、`lift=40`；WebGL `radius=0.12`、`segments=48`、`maxPixelRatio=min(devicePixelRatio, 2)`、纹理缓存 6 张。纸色/提亮色通过 `--grabfold-paper` / `--grabfold-lift` 跟随亮暗主题（见 `src/index.css`）。
- **布局**：书本使用 `fit: "contain"` 在阅读区域内居中，容器带 `env(safe-area-inset-bottom)` 内边距；grabfold 用 `ResizeObserver` 跟随窗口、全屏与旋转。页面按原始比例显示（`object-fit: contain`），不会拉伸。

单元测试：`src/lib/bookPages.test.ts`（1/2/3/奇数/偶数页、首末页、单双页切换、封面开关、RTL/LTR、position 与漫画页互转、预加载窗口）与 `src/lib/pageImages.test.ts`（去重、并发上限、LRU/字节预算、失败重试、销毁）。e2e（`e2e/reader-pages.mjs`）覆盖：书本模式挂载与 comimi 卸载、WebGL canvas、点击/键盘/拖动/flick/回弹取消翻页、页码列表跳转、双页映射、方向切换保页、返回平移模式保页，以及在禁用 WebGL 的上下文里回退到 DOM 折叠。

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

### 应用侧自绘面板动画

阅读器顶栏的「阅读设置 / 键盘快捷键 / 页面列表」面板使用项目自己的 `ReaderToolbar.Panel` 实现，没有沿用 comimi 的菜单/设置面板。为了让这些弹出面板也有和 comimi 内置面板一致的展开动效，参考 comimi 菜单面板的进入动画给 `Panel` 加了入场动画：

- `src/components/reader/ReaderToolbar.tsx`：给 `Panel` 容器加上 `reader-panel-enter` 类，在 `onAnimationEnd` 时清理类名。
- `src/index.css`：新增 `reader-panel-enter` 关键帧，面板从 `translateY(-12px)` 与 `opacity: 0` 垂直下落到正常位置与完全不透明，使用 `--reader-spring` 曲线；`prefers-reduced-motion: reduce` 时关闭动画。

### 应用侧自绘分段切换器动画

阅读设置面板里的「阅读方向 / 翻页动画 / 翻页模式」分段切换器原本用静态背景色表示选中项。为了与 comimi 内置的 `comimi-view-switcher` / `comimi-page-mode-indicator` 滑动指示器保持一致，改为由 `SegmentGroup` 单独渲染一个 `reader-segment-indicator` 背景元素：

- `src/components/reader/ReaderToolbar.tsx`：新增 `SegmentGroup` 组件，在 `useLayoutEffect` 里根据当前选中项计算对应按钮的 `offsetLeft` / `offsetWidth`，并设置到指示器上；切换时指示器通过 CSS `transform` 与 `width` 过渡平滑滑到新的位置。
- `src/index.css`：新增 `.reader-segment-indicator` 过渡样式，使用短平快的 `ease-out` 曲线，避免过冲；`prefers-reduced-motion: reduce` 时关闭动画。
- 首次渲染（如设置面板刚打开）时先临时把指示器的 `transition` 设为 `none`，定位到当前值并强制一次 reflow 后再恢复，因此指示器直接出现在选中项下方，不会从左边缘滑入/拉伸；只有之后用户切换选项时才会播放滑动动画。

这样阅读器设置面板的分段切换器在切换时会有平滑跟随的滑动指示器，但打开面板时不会从远处滑入或产生弹性过冲。

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

阅读器（comimi）的页面列表会为每一页创建缩略图，大画廊（上千页）会瞬间发起大量 `/api/image-cache/page-thumbnail` 请求。`loadThumbnails` 拦截这些图片，**不做视口懒加载**：缩略图出现即入队加载，通过并发上限为 5 的队列逐个请求；节点被移除时会取消占位并释放槽位。阅读器会给 `loadThumbnails` 传 `getLimit`，只预取已从 `/pages` 流式收到的页（`index < 已收页数`），新页到达时调用返回句柄的 `refresh()` 补入队，因此预取不会超过 `pages` 的数量。顶栏的页面列表（`ReaderPageList`）是用户主动打开的全页视图，索引寻址的缩略图由后端独立解析，不依赖 `/pages` 流，因此它在自己的容器上调用不带 `getLimit` 的 `loadThumbnails`；阅读器那份带 `getLimit` 的观察器会跳过 `.reader-page-list`，避免重复入队。

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
| `/settings` | 设置（含标签规则、用户管理、API Key） |
| `/gallery/:id/:token` | Gallery Detail |
| `/reader/:id/:token` | Reader |

列表页通过 `NavigationContext` + `useLastListRoute` 记住来源路由：进入 Gallery 详情时顶栏 / 抽屉高亮来源列表项，直接访问（无 referrer）时回落到首页。
