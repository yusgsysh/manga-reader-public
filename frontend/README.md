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

## 目录结构

```text
frontend/
├── e2e/                # Playwright 端到端脚本与 mock 服务
├── patches/            # comimi / comimi-react 本地补丁
├── public/             # 静态资源（favicon、内置标签翻译库）
├── scripts/            # 维护脚本（标签库更新）
└── src/
    ├── api/            # 后端接口客户端（按资源分模块，只负责请求与响应类型）
    ├── components/     # UI 组件，按业务域分组
    │   ├── ui/         # 无业务含义的基础展示组件（Chip / Skeleton / Toast …）
    │   ├── common/     # 跨页面复用的应用级组件（错误态 / 加载 / JumpSeek …）
    │   └── auth/ gallery/ layout/ reader/ search/ settings/ tag/
    ├── context/        # React Context 与 Provider（auth / navigation / theme）
    ├── domain/         # 纯业务规则，不依赖 React 组件、不发请求
    │   ├── gallery/    # 分类、标签、列表 URL 状态、高级搜索参数
    │   ├── reader/     # 阅读页模型、阅读偏好、进度保存
    │   └── prefill/    # 下载任务状态与进度展示规则
    ├── hooks/          # 可复用 React Hooks（数据获取、浏览器交互、页面状态）
    ├── lib/            # 与具体业务无关的通用工具（时间、主题色、图片、WebAuthn …）
    ├── pages/          # 路由页面（只做页面编排）
    ├── router/         # 路由表
    ├── services/       # 有状态、非 React 的服务（标签翻译数据库）
    └── types/          # 跨模块共享类型
```

依赖方向：`pages` / `components` / `hooks` → `domain` → `lib`。`domain` 不依赖 `components` 或 `hooks`；`lib` 不承载业务规则。

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

本仓库对 `@yui540/comimi` 维护四处补丁，均由同一个 `patches/@yui540%2Fcomimi@0.26.0.patch` 承载：**预加载页白屏**、**触屏进度条缩略图**、**全屏入口与移除被应用顶栏替代的原生 UI**、**平移翻页动画入口统一**。另有 `patches/@yui540%2Fcomimi-react@0.3.0.patch`，把新增的 `fullscreenRequest` 事件加入运行时事件白名单，使 `onFullscreenRequest` 能作为 props 透传。

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

### 补丁三：全屏入口与移除被应用顶栏替代的原生 UI

#### 背景

阅读器的返回、全屏、阅读设置、页面列表和快捷键面板都由应用顶栏（`ReaderToolbar`）提供。comimi 自带的菜单胶囊、菜单面板、设置层、左右箭头和 dock 按钮此前只是被 `src/index.css` 隐藏，代码仍会被构建、实例化并占用键盘（`M` / `S`）。现在直接删除这些原生 UI 的构建代码与接线，只保留应用实际使用的进度条 dock。

#### comimi 侧补丁

- **事件**：`dist/types.d.ts` 的 `ViewerEventMap` 只新增 `fullscreenRequest`（`back` 事件链随菜单一起删除）；`dist/index.js` 的核心回调新增 `requestFullscreen` 并 `emit`，没有监听者时回退到 `setLayoutMode("browserFullscreen")`。
- **全屏入口**：`F` 键改为派发 `fullscreenRequest`（应用顶栏的全屏按钮直接调用同一个 `toggleFullscreen`），由应用侧统一处理（保持原来的 `browserFullscreen` + 原生 `requestFullscreen` + `fullscreenchange` 同步）。
- **移除状态提示**：删掉 `setLayoutMode` 的布局模式 toast（进入/退出全屏时弹出的「全屏」）、`toggleAutoPageTurn` 的自动播放开始/停止 toast，以及 `notifyFavoriteAdded` 的收藏 toast；应用自身不调用 `notify()`，因此 `.comimi-notifications` 不再出现。
- **删除被顶栏替代的 UI**：菜单面板 `Ce`（菜单/页面列表/收藏/快捷键/分享/关于视图与菜单兔）、设置层 `ke`、左右箭头 `we`、视图切换器 `je`，以及 dock 的 `comimi-controls-row`（autoplay / page-mode / settings 按钮）。renderer 不再创建或插入它们，`applyOverlayVisibility` 与点击吞掉选择器也移除了对应类名。
- **移除宽屏 UI**：快捷键列表与键盘 `W` 分支删除；视图切换器随 `je` 整体删除。`LayoutMode` 类型与 `setLayoutMode("wide")` 仍保留给 API 使用，但不再有 UI 入口。

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
+		requestFullscreen: () => {
+			this.events.has("fullscreenRequest") ? this.events.emit("fullscreenRequest", void 0) : this.setLayoutMode("browserFullscreen");
 		}
 	};
```

#### comimi-react 侧补丁

`dist/index.js` 的事件白名单 `V` 增加 `"fullscreenRequest"`。comimi-react 的 `on<EventName>` props 类型由 comimi 的 `ViewerEventMap` 推导，运行时却由这份硬编码白名单决定，因此两处必须同时加上，`onFullscreenRequest` 才会真正生效；它也会把这个 prop 从透传到 DOM 的 rest props 中剔除。

#### 应用侧

`ReaderPage` 使用应用顶栏提供返回（`flushProgress()` + `goBack()`，沿用 `useBackNavigation` 的浏览器历史 / 深链回退规则）与全屏按钮，`<MangaViewer onFullscreenRequest={...} />` 只接收 comimi 的 `F` 键全屏请求；`settings.layoutMode` 默认 `inline`（标准）。

### 补丁四：平移翻页动画入口统一

#### 背景

comimi 原生的平移翻页只在触摸滑动结束时的「惯性翻页」路径触发；点击屏幕左右两侧和键盘翻页都是即时跳转、没有任何过渡。

#### 补丁

核心 `nextPage` / `previousPage` 原来直接 `commitNextPage` / `commitPreviousPage`（点击、键盘都走这里），改为先走 `renderer.requestPageTurn(state, direction)`（即 `animatePageTurn`），仅在无法动画（已是边界、正在动画中）时回退到直接提交；动画期间 `isPageTurnAnimating` 挡住重复触发。自动播放也改走 `nextPage()`，因此与手动翻页使用同一条平移动画。

### 已删除被应用顶栏替代的 comimi 原生 UI 与控制键

下次升级重打补丁时，以下内容应继续删除，否则它们会被构建出来但被 CSS 隐藏，或占用键盘：

| comimi 原生 UI | 原类/入口 | 处理 |
| --- | --- | --- |
| 浮动菜单胶囊、菜单面板（页面/收藏/快捷键/分享/关于、菜单兔、返回入口） | `Ce` / `.comimi-menu-panel` / `.comimi-menu-top` | 删除 |
| 设置层（语言/主题/封面/方向/间隔） | `ke` / `.comimi-settings-layer` | 删除（应用设置面板已覆盖） |
| 左右侧箭头 | `we` / `.comimi-arrows` | 删除（点击区域、按键和进度条已覆盖翻页） |
| dock 的自动播放、单页/双页、视图切换、设置按钮 | `Me.buildRow` / `.comimi-controls-row` / `je` / `.comimi-autoplay` / `.comimi-page-mode` / `.comimi-view-switcher` / `.comimi-settings` | 删除（顶栏与设置面板已覆盖） |
| dock 进度条与触摸预览 | `Me.buildSeek` / `.comimi-seek*` | **保留** |
| 键盘 `M`（菜单）与 `S`（设置） | `bindKeyboard` | 删除（无对应面板，应用顶栏接管菜单） |
| 键盘 `N`（标准）与 `C`（封面） | `bindKeyboard` | 删除（应用改用 `C` 切换跨页） |
| 键盘 `Esc` | `bindKeyboard` | 删除（面板关闭与退出阅读器由应用顶栏接管） |
| 键盘 `←` / `→` / `Space` / `A` / `F` / `P` / `O` | `bindKeyboard` | **保留**（分别翻页、自动播放、全屏请求、跨页、控制层），其中 `F` / `P` / `O` 由应用侧映射覆盖 |
| 状态提示 toast（全屏 / 标准、自动播放开始 / 停止、收藏） | `notify(...)` / `.comimi-toast` | 删除调用，不再弹出 |

第二轮清理继续删除只服务上述 UI 的传递性代码：

- 设置面板专用 inputs：checkbox（`fe` / `pe`）、rolling switch（`me`）、selectbox（`he`）、range slider（`ge`），以及它们的 SVG 命名空间/常量。
- 完整 icons 区块（`b` / `x` / `S` / `C` / `se` / `ce` / `le`）：删除后已无调用方。
- 菜单兔 SVG 与工厂（`ve` / `ye`）以及菜单滚动渐隐 `D`。
- about 视图专用的版本/链接常量（`m` / `h` / `g`）。

**保留**默认 logo 与 splash：`y` / `ae` / `oe` / `v` / `_` / `ee` / `te` / `ne` / `re` / `ie` 仍被 splash 的默认 logo 使用，不能随 about 视图一起删除。

库内 i18n 文案、`hiddenSettings` / `forceSettings` / `ViewerState.panel` / `setPanel()` 等公共 API 仍保留（comimi-react 类型依赖它们），但当前 UI 已没有消费者；如上游后续清理可一并简化。bundle 内联 CSS 中已删组件的样式规则（菜单/设置/箭头/view-switcher/autoplay 等）也仍保留，属于可选的体积清理。

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
   # 按本文各补丁的 diff 修改
   # node_modules/@yui540/comimi/dist/index.js 与 dist/types.d.ts
   # 同时确认「已删除被应用顶栏替代的 comimi 原生 UI」表中的类/键位仍未出现
   bun patch --commit 'node_modules/@yui540/comimi'
   ```

   bun 会生成新的 `patches/@yui540%2Fcomimi@x.y.z.patch` 并更新 `patchedDependencies`；确认无误后删除旧补丁文件。

3. 若 `@yui540/comimi-react` 也升级，重新打事件白名单补丁：

   ```bash
   bun patch @yui540/comimi-react@x.y.z
   # 确认 node_modules/@yui540/comimi-react/dist/index.js 的事件白名单 V 中
   # 仍包含 "fullscreenRequest"（"back" 已随菜单删除）
   bun patch --commit 'node_modules/@yui540/comimi-react'
   ```

4. 验证：`bun run lint && bun run build && bun run test && bun run test:e2e`。

5. 建议同时向上游提 issue/PR（合并两条渲染分支、补齐触摸事件、暴露 fullscreen 事件、允许应用接管/移除原生菜单与设置面板），最终移除补丁。

### 应用侧自绘面板动画

阅读器顶栏的「阅读设置 / 键盘快捷键 / 页面列表」面板使用项目自己的 `ReaderToolbar.Panel` 实现；comimi 内置的菜单/设置面板已随补丁删除。为了让这些弹出面板有统一的展开动效，给 `Panel` 加了入场动画：

- `src/components/reader/ReaderToolbar.tsx`：给 `Panel` 容器加上 `reader-panel-enter` 类，在 `onAnimationEnd` 时清理类名。
- `src/index.css`：新增 `reader-panel-enter` 关键帧，面板从 `translateY(-12px)` 与 `opacity: 0` 垂直下落到正常位置与完全不透明，使用 `--reader-spring` 曲线；`prefers-reduced-motion: reduce` 时关闭动画。

### 应用侧自绘分段切换器动画

阅读设置面板里的「阅读方向 / 翻页模式」分段切换器原本用静态背景色表示选中项。为了让切换有平滑跟随的滑动指示器，改为由 `SegmentGroup` 单独渲染一个 `reader-segment-indicator` 背景元素：

- `src/components/reader/ReaderToolbar.tsx`：新增 `SegmentGroup` 组件，在 `useLayoutEffect` 里根据当前选中项计算对应按钮的 `offsetLeft` / `offsetWidth`，并设置到指示器上；切换时指示器通过 CSS `transform` 与 `width` 过渡平滑滑到新的位置。
- `src/index.css`：新增 `.reader-segment-indicator` 过渡样式，使用短平快的 `ease-out` 曲线，避免过冲；`prefers-reduced-motion: reduce` 时关闭动画。
- 首次渲染（如设置面板刚打开）时先临时把指示器的 `transition` 设为 `none`，定位到当前值并强制一次 reflow 后再恢复，因此指示器直接出现在选中项下方，不会从左边缘滑入/拉伸；只有之后用户切换选项时才会播放滑动动画。

这样阅读器设置面板的分段切换器在切换时会有平滑跟随的滑动指示器，但打开面板时不会从远处滑入或产生弹性过冲。

### 面板打开时保持底部进度条

底部进度条是 comimi 的 controls dock，由库内的 overlay 自动隐藏计时器控制（约 3 秒无操作后隐藏）。应用侧面板渲染在 comimi 根节点之外，不会刷新该计时器，因此面板还开着时进度条会先消失，而顶栏和面板仍然可见。

- `src/index.css`：用 `.reader-shell:has(.reader-panel)` 在应用侧面板打开期间覆盖 dock 的 `[data-overlay="false"]` 隐藏样式，让进度条与顶栏一起保持可见；面板关闭后恢复 comimi 原有的隐藏过渡。
- `e2e/reader-pages.mjs`：打开阅读设置并等待超过自动隐藏时间，断言 dock 仍可见，防止回归。

### 应用侧键盘映射与全屏

阅读器的键盘映射由应用顶栏（`ReaderToolbar`）在 `window` 的捕获阶段统一处理，并 `preventDefault` / `stopPropagation`，避免 comimi 内置的 `F` / `P` / `O` 绑定同时触发：

| 键 | 行为 |
| --- | --- |
| `F` | 切换全屏 |
| `Esc` | 先关闭打开的面板；没有面板时退出阅读器，返回画廊详情 |
| `M` | 显示 / 隐藏菜单（顶栏控制层）；面板打开时一并关闭 |
| `C` | 单页 / 跨页 |
| `P` | 打开 / 关闭页面列表 |
| `N` / `O` | 已删除：事件被吞掉，不再触发布局或控制层切换 |

输入框、下拉框和 `contenteditable` 中不拦截；带 `Ctrl` / `Meta` / `Alt` 的组合键留给浏览器。

全屏封装在 `src/lib/fullscreen.ts`：兼容标准与 `webkit` / `ms` 前缀 API。进入时对 `.reader-shell`（而非 `documentElement`）请求原生全屏，顶栏因此仍在全屏子树内；浏览器不支持或拒绝时仍切换到 comimi 的 `browserFullscreen` 布局，按钮和 `F` 不会变成空操作。`fullscreenchange` / `webkitfullscreenchange` / `MSFullscreenChange` 都会同步顶栏状态，离开路由时主动退出原生全屏。

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

相关实现：`src/hooks/useInfiniteScroll.ts`、`src/components/common/InfiniteScrollTrigger.tsx`、`src/components/common/JumpSeekPanel.tsx` / `JumpSeekMenu.tsx`、`src/domain/gallery/jumpSeek.ts`、`src/lib/thumbnails.ts`，以及 `src/hooks/useGalleryList.ts`、`useSearch.ts`、`useBookshelf.ts`、`useRecentlyRead.ts`（均为 `useInfiniteQuery`）。

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
