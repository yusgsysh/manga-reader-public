# Manga Reader 桌面版（Wails）

`backend/cmd/desktop` 是一个 Wails 桌面壳：它在**同一个进程**里启动与 Web 版完全相同的 Gin REST API（`internal/app`），再把现有 React 前端塞进 WebView。

没有第二套接口——前端在浏览器里能做的每件事，在这里走的都是同一套 `/api/*`。

```
WebView (wails://)
   │  GET /settings、/api/… 、静态资源
   ▼
desktopAssets.Middleware            ← assets.go
   ├─ /api/*、/healthz  ──反向代理──▶ Gin（127.0.0.1:<随机端口>）→ internal/app
   ├─ 真实静态文件                   → Wails 资源服务器（嵌入的 frontend/dist）
   └─ 其余路径                       → index.html（SPA 路由，已注入 API 地址）
```

---

## 依赖

- Go 1.27+、Bun
- [go-task](https://taskfile.dev)：`go install github.com/go-task/task/v3/cmd/task@latest`
- [Wails v3 CLI](https://wails.io)：`go install github.com/wailsapp/wails/v3/cmd/wails3@latest`
- Linux 需要 GTK3 与 `webkit2gtk` 开发头（Wails v3 默认使用 4.1）

构建逻辑全部由 `build/` 下的 [Taskfile](https://taskfile.dev) 驱动，`wails3` 只是这些 task 的入口；项目本身不再使用 `wails.json`。
应用元信息（名称、包名、版本）在 [`build/config.yml`](build/config.yml)。

---

## 构建 / 运行 / 开发

```bash
cd backend/cmd/desktop
wails3 build     # 产物：bin/manga-reader-desktop
wails3 dev       # 开发模式：Vite 热更新 + 运行时注入 API 地址
wails3 task run  # 运行已构建的桌面程序
```

`wails3 build` 会依次执行 Taskfile 中的：

| 阶段 | 命令 |
|------|------|
| 安装前端依赖 | `bun install` |
| 编译前端 | `bun run build:desktop`（即 `vite build --mode desktop`） |
| Go 编译 | `go build -tags production` |

前端产物 `frontend/dist` 会被复制到 `backend/cmd/desktop/dist/`，再由 [`main.go`](main.go) 的 `//go:embed all:dist` 嵌进二进制，编译完成后该目录还原为只剩 `.gitkeep`。因此**二进制是自包含的**：不需要 nginx、不需要 Node，双击即可运行。

窗口标题 `Manga Reader`。

### 不用 Wails CLI 的构建方式

```bash
cd frontend && bun install && bun run build:desktop   # 先产出 dist/
cp -r dist ../backend/cmd/desktop/dist/
cd ../backend && go build -tags production -o desktop ./cmd/desktop
```

（没有 `dist/` 时 `go:embed` 会失败；`dist/` 至少要含 `.gitkeep`。）

---

## Android（APK / AAB）

Android 目标会把 Go 代码编成 `libwails.so`（`-buildmode=c-shared`，走 NDK），再由 Gradle 打包成 APK/AAB。相关 task 在 [`build/android/Taskfile.yml`](build/android/Taskfile.yml)。

### 本地构建

需要 Android SDK、NDK（`26.3.11579264`）和 JDK 21；SDK 路径取 `ANDROID_HOME` / `ANDROID_SDK_ROOT`，或默认的 `~/Android/Sdk`。

```bash
cd backend/cmd/desktop
task android:build            # 仅编译 libwails.so（默认按宿主架构）
task android:assemble:apk     # 打 debug APK → bin/manga-reader-desktop.apk
task android:package          # 打 release APK（默认 arm64，真机用）
task android:package:fat      # release APK，含 arm64 + x86_64
task android:bundle           # release AAB（Play 上传用）
```

### 用 Podman/Docker 构建（推荐，宿主机无需装 SDK/NDK）

仓库根目录的 [`build-android-docker.sh`](../../../build-android-docker.sh) 在容器里完成全部构建，再把 APK 拷出来：

```bash
./build-android-docker.sh                              # debug APK（宿主架构）
ARCH=arm64 ./build-android-docker.sh                   # 指定目标架构
TARGET=android:package ./build-android-docker.sh       # release APK
CONTAINER_ENGINE=docker ./build-android-docker.sh      # 用 docker 代替 podman
```

脚本最终产物为仓库根目录的 `manga-reader-desktop.apk`。镜像定义见 [`build/docker/Dockerfile.android`](build/docker/Dockerfile.android)。

> **架构很关键**：`android:build` 默认使用宿主架构，所以在 x86_64 机器上默认产出的是 `x86_64` APK，**装不到 arm64 真机**（报 `INSTALL_FAILED_NO_MATCHING_ABIS`）。真机请传 `ARCH=arm64`，或用 `android:package:fat` / `TARGET=android:package:fat` 打同时包含两种 ABI 的包。模拟器则用宿主架构（x86_64 主机上的 `x86_64`）即可。

---

## 数据、日志与配置文件

全部落在用户的配置/缓存目录下（Linux 对应 `~/.config` 与 `~/.cache`），与 Web 版的 `./data` 完全隔离：

| 内容 | 路径 |
|------|------|
| SQLite 数据库 | `~/.config/manga-reader/manga-reader.db` |
| 日志 | `~/.config/manga-reader/manga-reader.log`（JSON 行，stdout 同时输出） |
| 配置文件 `config.env` | `~/.config/manga-reader/config.env` |
| 图片缓存 | `~/.cache/manga-reader/cache` |

`os.UserConfigDir()` / `os.UserCacheDir()` 不可用时回退到相对目录 `data/manga-reader/`。

**日志是桌面版唯一的诊断手段**，所以打不开日志文件时会退化成「只写 stdout」而不是静默丢弃，并在启动时记一条 warning。

---

## 配置优先级

```
应用内「设置」页保存的值（SQLite settings 表）   ← 最高，立即生效
        ▲ 覆盖
进程环境变量
        ▲ 覆盖
config.env（MANGA_READER_CONFIG_FILE 可改路径）
        ▲ 覆盖
applyDesktopDefaults() 注入的桌面默认值          ← 最低
```

桌面默认值（全部是「未设置才生效」，显式环境变量总是赢）：

| 变量 | 桌面默认 |
|------|----------|
| `MANGA_READER_DB_DRIVER` | `sqlite` |
| `MANGA_READER_DB_PATH` | `~/.config/manga-reader/manga-reader.db` |
| `MANGA_READER_STORAGE_DRIVER` | `local` |
| `MANGA_READER_STORAGE_DIR` | `~/.cache/manga-reader/cache` |
| `ENVIRONMENT` | `desktop` |
| `LOG_LEVEL` | `info` |

`config.env` 是 `KEY=VALUE` 行格式，`#` 开头为注释，值两侧的引号会被剥掉；**已存在的真实环境变量优先于文件**。它是给不想碰 shell 的用户准备的，绝大多数配置用应用内设置页更方便。

### 应用内「设置」页

启动后在 `/settings` 打开，或直接调 API：

```bash
# 读取（密钥显示为 ********）
cat /tmp/manga-reader-desktop.json        # 拿 apiBaseUrl
curl -s "$API/api/settings"

# 保存：PATCH 语义，字段缺省即「不变」；******** 表示「保持原值」
curl -s -X PUT "$API/api/settings" -H 'Content-Type: application/json' \
  -d '{"cookie":{"memberId":"42","passHash":"xxxxxxxx"}}'
```

覆盖的配置项与行为约定（存数据库、立即热生效、只存与环境变量的差异）见 [`README.md`](../../../README.md) 的「运行时设置」与 [`API.md`](../../API.md) 第 28 节。

因此**首次运行不需要预置 Cookie**：后端会打一条 warning 照常启动，在设置页填上即可。

---

## 前端如何拿到 API 地址

Gin 绑定的是 `127.0.0.1:0`（随机端口），前端必须在运行时才知道：

1. **打包版**：启动时把端口写成 JSON，`injectRuntimeConfig` 在 `</head>` 前插入
   `<script>window.__MANGA_READER_CONFIG__={"apiBaseUrl":"http://127.0.0.1:38587"}</script>`。
   前端 `src/api/client.ts` 优先读它。
2. **开发版（`wails dev` / `bun run dev:desktop`）**：Vite 插件 `manga-reader:runtime-config` 读同一份 JSON 注入，因此不需要任何反向代理。
3. 回退顺序：`window.__MANGA_READER_CONFIG__` → `import.meta.env.VITE_API_BASE_URL` → `http://localhost:8080`。
   桌面构建的 [`frontend/.env.desktop`](../../../frontend/.env.desktop) 把 `VITE_API_BASE_URL` 置空，表示「同源」，WebView 落到 shell 自己的 `/api` 代理上。

后端 `CORSMiddleware` 对任意来源返回 `Access-Control-Allow-Origin: *`，开发模式直连 `127.0.0.1:<port>` 也不会被拦。

### 运行时 JSON

```
默认：$TMPDIR/manga-reader-desktop.json        # Linux 通常是 /tmp/manga-reader-desktop.json
内容：{"apiBaseUrl":"http://127.0.0.1:38587"}
覆盖：MANGA_READER_RUNTIME_FILE=/path/to.json
```

启动时原子写入（`tmp` + `rename`），退出时 `defer os.Remove` 删除。测试与多实例场景用 `MANGA_READER_RUNTIME_FILE` 隔离。

---

## 退出与清理

- WebView 关闭 → Wails `OnShutdown` → `app.Shutdown`（15 秒超时）→ 关闭数据库/存储 → 删除运行时 JSON。
- 直接 `SIGTERM` 同样走这条路径（Wails 负责把信号转成 shutdown），日志文件由 `closeLogger` 关闭。
- Bindings 由 `wails3 generate bindings` 在编译前静态生成（见 [`build/Taskfile.yml`](build/Taskfile.yml) 的 `generate:bindings`），不再通过 `-tags bindings` 编译副本来导出。

---

## 测试

```bash
cd backend
go test ./...          # 覆盖 cmd/desktop（assets_test.go、e2e_test.go）
go vet ./...
gofmt -l .
```

- `assets_test.go`：中间件路由、`index.html` 注入、运行时 JSON 读写、日志/目录兜底。
- `e2e_test.go`：用 `fstest.MapFS` 造一个最小前端，把中间件架在**真实的** `internal/app` 前面，验证 WebView 形状的请求能走完与 Web 版相同的 handler 链。

前端：

```bash
cd frontend
bun run lint && bunx tsc -b && bun run test && bun run build:desktop
```

### 手工冒烟

```bash
Xvfb :99 -screen 0 1280x800x24 &          # 无显示环境
DISPLAY=:99 dbus-run-session -- ./bin/manga-reader-desktop &
cat /tmp/manga-reader-desktop.json
curl -s $(python3 -c "import json;print(json.load(open('/tmp/manga-reader-desktop.json'))['apiBaseUrl'])")/healthz
```

`HOME`/`XDG_CONFIG_HOME`/`XDG_CACHE_HOME` 可以指到临时目录，避免污染真实用户数据。

---

## 排障

| 现象 | 处理 |
|------|------|
| 编译报 `webkit2gtk` 找不到 | 安装 `webkit2gtk4.1-devel` / `libwebkit2gtk-4.1-dev`；若发行版只有 4.0，构建时通过 `EXTRA_TAGS` 指定对应 webkit tag（见 Wails v3 文档） |
| 启动即报找不到前端资源 | 先 `cd frontend && bun run build:desktop` 并同步 `dist/`；或直接用 `wails3 build`（它会替你跑） |
| 白屏 | 看 `~/.config/manga-reader/manga-reader.log`；确认 `index.html` 里有 `window.__MANGA_READER_CONFIG__` |
| 前端连不上 API | `cat` 运行时 JSON 确认端口仍在；`curl $API/healthz`；退出残留的 JSON 说明上一次没正常结束 |
| 找不到配置/数据库 | 实际路径由 `os.UserConfigDir()` 决定，日志首行的 `db_driver`/`storage_driver` 可以佐证 |
| 想换个数据目录 | 用 `MANGA_READER_DB_PATH`、`MANGA_READER_STORAGE_DIR`、`MANGA_READER_CONFIG_FILE` 覆盖，或把整个 `HOME` 指走 |
| 装了 APK 但报「应用不兼容」/ `INSTALL_FAILED_NO_MATCHING_ABIS` | APK 的 ABI 与设备不符：真机需要 arm64，改用 `ARCH=arm64` 或打 fat 包（见上「Android」） |
