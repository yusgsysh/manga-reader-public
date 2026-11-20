# 后端开发

```bash
cd backend
go test ./...
go vet ./...
go run .
```

`go run .` 只提供 API：静态前端通过 `//go:embed` 在编译期打包进 `internal/web/dist`，源码检出默认只有占位文件（`.gitkeep`），此时后端以 API-only 模式启动并打一条 warning。要在本地同时看到完整页面：

```bash
# 方式一：Docker 构建（CI 同款，前端由镜像构建自动嵌入）
docker build -t manga-reader .

# 方式二：本地 go run 前先放一份前端产物
cd ../frontend && bun install && bun run build
rm -rf ../backend/internal/web/dist && cp -r dist ../backend/internal/web/dist
cd ../backend && go run .
```

## Ent ORM 代码生成

后端使用 [Ent](https://entgo.io/) 作为 ORM（Schema as Code）。Schema 定义在 `internal/ent/schema/` 目录下。

修改 Schema 后需重新生成代码：

```bash
cd backend
go generate ./internal/ent/...
```

生成的代码位于 `internal/ent/` 下（由 Ent 自动生成，勿手动编辑）：

| 目录 | 说明 |
|------|------|
| `schema/` | 数据模型定义（手动编写） |
| `bookshelf/` | Bookshelf 字段常量与查询辅助 |
| `readingprogress/` | ReadingProgress 字段常量与查询辅助 |
| `prefilljob/` | PrefillJob 字段常量与查询辅助 |
| `gallerycache/` | GalleryCache 字段常量与查询辅助 |
| `migrate/` | 数据库迁移逻辑 |

Schema 文件：
- `internal/ent/schema/bookshelf.go` — 书架模型
- `internal/ent/schema/reading_progress.go` — 阅读进度模型
- `internal/ent/schema/prefill_job.go` — 离线下载任务模型
- `internal/ent/schema/gallery_cache.go` — 画廊缓存模型

后端默认监听 `127.0.0.1:8080`（仅回环；容器内为 `:8080`），完整 API 文档见 `backend/API.md`。

## 图片代理与缓存

| API | 数据来源 | MinIO | 用途 |
|-----|----------|:-----:|------|
| `/api/image/page` | ExHentai | ❌ | 阅读页图片代理（live） |
| `/api/image/thumbnail` | ExHentai | ❌ | 封面缩略图代理（live） |
| `/api/image/page-thumbnail` | ExHentai | ❌ | 页面缩略图裁剪（live） |
| `/api/image-cache/page` | MinIO / ExHentai | ✅ | 阅读页图片缓存 |
| `/api/image-cache/thumbnail` | MinIO / ExHentai | ✅ | 封面缩略图缓存（前端使用） |
| `/api/image-cache/page-thumbnail` | MinIO / ExHentai | ✅ | 页面缩略图（精灵图缓存 + 裁剪持久化，前端使用） |

缩略图缓存 Key 为 `thumbnail/<sha256(完整 URL)>`，与页面图片缓存（`images/`）相互独立。`/api/image/page-thumbnail` 与 `/api/image-cache/page-thumbnail` 支持两种寻址：`url`+`x/y/w/h`，或 `id`+`token`+`index`；精灵图缓存 Key 为 `sprite/<sha256(精灵图 URL)>`，裁剪结果持久化为 `crop/<sha256(精灵图 URL + rect)>` —— 每个格子终身只解码一次，后续请求直接命中裁剪产物（精灵图 URL 过期换新时 Key 自然失效，旧裁剪随 bucket 生命周期约 30 天过期）。冷缓存下同一精灵图不同格子的并发裁剪共享同一次解码。

## 缓存模型（无 TTL + read-through）

后端缓存**没有 TTL**：`gallery_cache` 一旦写入就被永久信任，命中即返回，不再按时间回源。

- **在线端点**（`/api/gallery/*`）：**每次回源上游**，只读不写缓存 —— 保证在线时永远是最新数据。
- **上游请求短共享**：同一 `client + 方法 + URL (+表单)` 的上游页面请求（画廊 HTML、种子页、gdata 元数据）会合并并发请求，并在 **10 秒**内复用同一次抓取（`backend/internal/exhentai/fetchcache.go`），因此详情 / 页列表 / `.gpc` 总数 / 单页缩略图等多个入口不会重复抓同一个页面。这是进程内 singleflight + 短 TTL 去重，不是持久缓存。
- **缓存端点**（`/api/gallery-cache/*`）：**read-through** —— 命中直接返回（`/pages` 以与在线一致的 NDJSON 流回放），未命中则流式回源，完整成功后整份回填。
- **种子**（`/api/gallery/:id/:token/torrents{,/…}`）：后端用登录 Cookie 实时抓取并代理下载，**不缓存**。
- **图片**统一走 `/api/image-cache/*`。

缓存只在以下事件发生时更新：

| 触发 | 动作 |
|------|------|
| 缓存端点 miss | 流式回源，完整成功后整份替换 `pages` + `thumbnails` |
| 图片永久失败（旧 page URL 失效，含非 200） | 从 URL 解析 gid → 按 `gallery_id` 反查 token → 后台整份刷新（5xx 视为瞬态不触发） |
| 缓存精灵图过期（sprite 404） | 本次请求内：整份刷新 → 若 URL 未变则单页直抓绕过 10s 文档短共享 → 按 index 回填 → 重试一次 |
| 打开时 `.gpc` 页数变化 | 后台异步（SWR）比对总页数，变化则整份替换（允许缩短） |
| 单页缩略图几何缺失 / 解析超时直抓 | 按 index 稀疏补洞 |

页面 URL 与缩略图几何都支持**按 index 增量合并**（非空覆盖、空保留、不缩短）；页数变化时改为整份替换。`*_fetched_at` 仅作观测写入，不参与命中/刷新判定。

精灵图过期自愈按 `galleryID:token:index` 合并整条流程：并发请求共享一次刷新、一次直抓与一次旧/新精灵图下载，因此过期后首个请求即成功，而不是先 502 再后台刷新。

前端图片加载失败时**直接失败、不自动重试**；失败已触发后端后台刷新，用户重新打开页面即可拿到新列表（页面缩略图的精灵图过期例外，由后端在请求内自愈）。

| 层 | 项 | 值 | 说明 |
|----|----|----|------|
| 后端 | ExHentai listing cursor | 10 分钟 | 内存中的下一页游标 |
| 后端 | 上游页面/元数据请求短共享 | 10 秒 | 进程内 singleflight + TTL 去重（`fetchcache.go`） |
| HTTP | live 图片代理（`/api/image/*`） | `max-age=3600` | `Cache-Control` |
| HTTP | MinIO 内容寻址图片（`/api/image-cache/*`） | `max-age=31536000, immutable` | `Cache-Control` |
| HTTP | `/pages` 流、`/api/gallery-cache/*` | `no-store` | 不缓存 |
| MinIO | 页面图片对象 | 约 30 天 | 由 MinIO bucket 生命周期策略控制，非代码常量 |
| 前端 | react-query `staleTime` | 见 `cacheConfig.ts` | 全局 30s；pages 10m；gallery / detail / search 5m；bookshelf / recently-read / list 2m；progress / prefill 30s；settings 0 |

> 注：`backend/internal/ttl` 现在只保留 HTTP `Cache-Control` 与 listing cursor；代码中另有一批**超时/预算**常量（如 `/pages` 抓取硬上限、缩略图解析超时、上游文档超时），它们限制单次操作的耗时，并非缓存 TTL。上游页面请求的 10 秒短共享见 `backend/internal/exhentai/fetchcache.go`（进程内去重，不落库）。

## 可观测性（Prometheus 指标）

`GET /metrics` 暴露 Prometheus 文本格式（回环限制或独立 Metrics Token；指标实现见 `backend/internal/metrics`，使用包私有 registry，测试可隔离采集）。可视化与抓取见 [自部署监控（Prometheus + Grafana）](../README.md#自部署监控prometheus--grafana)。

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `exhentai_upstream_requests_total` | Counter | `endpoint, method, status_code, outcome` | 上游请求。`outcome` 是**传输层**结果（ok = 拿到响应，**含 4xx/5xx**；error = 连接/超时失败），内容级失败见 `classification_total` |
| `exhentai_upstream_request_duration_seconds` | Histogram | `endpoint, method` | 上游延迟（send → 响应头） |
| `exhentai_upstream_response_bytes_total` | Counter | `endpoint` | 上游响应体**实际读取**字节数（透明 gzip 按解压后计；body 未被读取则不计） |
| `exhentai_upstream_retries_total` | Counter | `endpoint, reason` | 上游重试放大：`reason` = thumbnail / nl_fallback |
| `exhentai_upstream_inflight_requests` | Gauge | — | 正在进行中的上游请求数（send → 响应头） |
| `exhentai_upstream_classification_total` | Counter | `endpoint, outcome, code` | 内容级判定（sad_panda / ip_banned / http_error / error[文档解析失败]）+ 观察到的 HTTP 状态码 |
| `exhentai_fetchcache_requests_total` | Counter | `result` | 10 秒文档短共享：hit / miss / coalesced |
| `exhentai_fetchcache_entries` | Gauge | — | 短共享缓存当前条目数（写入与过期删除都会更新） |
| `image_cache_requests_total` | Counter | `kind, result` | MinIO 图片缓存查找：`kind` = page_image / thumbnail / sprite / page_crop，`result` = hit / miss |
| `gallery_cache_requests_total` | Counter | `kind, result` | `gallery_cache` 查找：`kind` = gallery / details / pages / page_thumb，`result` = hit / miss |
| `minio_cache_requests_total` | Counter | `op, result` | MinIO 操作：`op` = get / put / head，`result` = ok / not_found / error（**miss 不是 error**） |
| `minio_cache_operation_duration_seconds` | Histogram | `op` | MinIO 操作延迟 |
| `http_requests_total` | Counter | `method, route, status_code` | 本应用 HTTP 请求（含认证 401/403 与限流 429；handler panic 由 Recovery 转成 500 后同样计入） |
| `http_request_duration_seconds` | Histogram | `method, route` | 本应用 HTTP 延迟（中间件入口 → 响应完成） |
| `membudget_reserved_bytes` | Gauge | — | 当前已预留的图片负载字节数（`MANGA_READER_MEM_BUDGET` 预算内） |
| `membudget_limit_bytes` | Gauge | — | 内存预算上限；`0` 表示不限制（默认） |
| `membudget_waiters` | Gauge | — | 正阻塞等待预算释放的操作数（>0 说明预算打满、在排队） |
| `membudget_acquire_wait_seconds` | Histogram | — | 等待内存预算授予的时长 |

`http_*` 的 `route` 标签口径：

- 注册过的路由取 gin 路由模板（基数有界）；
- 静态文件与 SPA 首页由 `NoRoute` 提供，没有模板，因此显式标记为 `route="/static"` 与 `route="/spa"`；
- 其余未匹配路径（真实 404）才归入 `route="unmatched"`；
- 长连接（SSE `/api/events/changes`、`/api/sync/events`，NDJSON `/pages` 流，ZIP 下载）只计数、**不进延迟直方图**——它们能开数小时，记录会把观测全塞进 `+Inf` 桶，让该路由 P95 恒等于最后一个桶。

此外注册了 Go runtime 与进程指标（`go_goroutines`、`go_memstats_*`、`go_gc_*`、`process_resident_memory_bytes`、`process_cpu_seconds_total`、`process_open_fds` 等），无需手工采集。

Hit rate 不单独存储为 gauge，用 `rate` 计算，例如：

```promql
sum(rate(image_cache_requests_total{result="hit"}[5m]))
  /
sum(rate(image_cache_requests_total[5m]))
```

`gallery_cache_requests_total` 同理；追加 `by (kind)` 或 `kind=` 过滤可按缓存类型拆分。计数口径：每次缓存查找计一次（hit 或 miss，读取错误不计）；缓存命中的图片/缩略图以流式直出（不经缓冲读取，仍计 hit）；gallery 缓存按端点实际判定（如 details 需 `DetailsFetchedAt` 非空、pages 需 `len(Pages) > 0` 才算命中）；sprite / crop 读取在 singleflight 合并后的实际读取上各计一次。离线兜底（书架）与批量装载（最近阅读）不计入。

HTTP 5xx 率（`route` 可按接口拆分；`/metrics` 抓取与 `/healthz` 探活恒为 200，过滤掉可避免稀释）：

```promql
sum(rate(http_requests_total{status_code=~"5..",route!~"/metrics|/healthz"}[5m]))
  /
sum(rate(http_requests_total{route!~"/metrics|/healthz"}[5m]))
```

## 阅读记录清理

阅读记录不会自动清理。手动调用：

```bash
# 删除 30 天以前（默认）
curl -X POST http://localhost:8080/api/reading-progress/cleanup
# 删除 7 天以前
curl -X POST http://localhost:8080/api/reading-progress/cleanup?days=7
# 删除全部
curl -X POST http://localhost:8080/api/reading-progress/cleanup?days=0
```

## 数据同步

多实例双向同步**阅读进度、书架与画廊缓存**（settings 不同步），所有连接由桌面端主动发起（NAT 友好）：

- **服务端**：`MANGA_READER_SYNC_ENABLED=true` 时挂载同步端点。
- **推荐：用户级同步 Key**：服务端在「设置 → API Key」为当前账号创建 Key（`POST /api/sync/keys`，只保存 SHA-256 哈希，明文只显示一次）。桌面端「数据同步」填入服务端地址 + 该 Key 并启用。服务端把收到的私有行全部归属到 Key 的账号，并且只返回该账号的数据（画廊缓存为公共元数据，双方共享），因此**两端不需要相同用户名或相同用户 UUID**。
- **桌面端 / 客户端**：设置页「数据同步」填服务端地址 + Key 并启用（配置存 `sync_state` 表，并绑定保存配置的当前账号）。请求使用 `Authorization: Bearer <Key>`，不再使用 Basic Auth 或全局同步令牌。
- 双方通过 `POST /api/sync/push` 推送 + 拉取，`GET /api/sync/events` SSE 监听对端新变更（写后 2s 去抖推送，心跳 25s，断线退避 1s→60s，60s 轮询兜底）。同步 Key 需要 `sync:write`（push）或 `sync:events`（SSE）。
- 合并规则：按行 LWW（`updated_at` 新者胜），删除用 tombstone；首次连接返回全量快照（同时带上 outbox 中未送达的删除 tombstone，避免首次同步把已删除的行重新复活）；更换 `server_url` 会自动重置同步游标与引导标记，对新对端重新全量（`gallery_cache` 行在 LWW 胜出时按字段合并，空值不覆盖，镜像本地 upsert 语义）。
- 设置页「数据同步」提供「全量同步」按钮（`POST /api/sync/resync`）：清空同步游标并唤醒引擎，下一次交换重新上传本端全量数据并拉取对端全量数据。用于长时间断连、对端重建或两端数据已经不一致时的手动修复。
- 同步 Key 是受信任的对端凭证，但只能同步它所绑定的账号：私有行按 Key 的账号落库，拉取只返回该账号的数据。请求里的 `user_id` 不能扩大范围，同步请求带 `request_id` 写入审计日志。
- 桌面端本地通过 `GET /api/events/changes` SSE 通知前端刷新（该端点使用用户 JWT）。

```bash
# 在服务端「数据同步」页为当前账号创建同步 Key 后，桌面端配置：
curl -X PUT http://localhost:8080/api/sync/config -H 'Content-Type: application/json' \
  -d '{"enabled":true,"server_url":"http://server:8080","token":"mrk_..."}'

curl http://localhost:8080/api/sync/status
```

端点与协议详见 `backend/API.md`「28. Data Sync」。

## 离线下载（Prefill）

画廊详情页的「添加下载任务」把页面 URL 列表交给后端，由后台 worker 逐页串行抓取并写入 MinIO 缓存（约 30 天过期）。前端「下载管理」页（`/downloads`）轮询进度，支持取消、删除、批量清理，并可随时按任务流式下载 ZIP。

启用 ZIP 归档存储（`ZIP_ENABLED=true`）后，任务在抓取完图片后还会**流式生成 ZIP 并直接上传到对象存储**：图片逐页写入 `archive/zip` 的 `zip.Writer`，通过 `io.Pipe` 直连分片上传，全程不落本地磁盘、不把完整 ZIP 载入内存。上传成功后把归档对象 Key / 大小 / ETag 记录在任务上（`archive_ready=true`），后续下载直接流式回源，重复下载不再重新打包。ZIP 归档存储与图片缓存完全独立：可分别关闭、使用不同 Bucket / Endpoint / 凭据，也可同时启用。默认下载走「后端流式代理」；设 `ZIP_PRESIGN_DOWNLOAD=true` 后改为 302 跳转到对象存储的**签名 URL 直下**（浏览器直接向存储拉取，节省后端带宽，但要求存储端点对浏览器可达，且仍需登录才能拿到该 URL）。

```bash
# 创建任务（同画廊已有排队/运行中的任务时返回该任务）
curl -X POST http://localhost:8080/api/prefill \
  -H 'Content-Type: application/json' \
  -d '{"gallery_id":123,"token":"abc","title":"My Gallery","urls":["https://exhentai.org/s/x/123-1"]}'

# 任务列表 / 单个任务（轮询进度；archive_ready 表示 ZIP 已入库）
curl http://localhost:8080/api/prefill
curl http://localhost:8080/api/prefill/1

# 取消 / 删除记录 / 批量清理（0 = 全部已结束记录）
curl -X POST http://localhost:8080/api/prefill/1/cancel
curl -X DELETE http://localhost:8080/api/prefill/1
curl -X POST 'http://localhost:8080/api/prefill/cleanup?days=30'

# 流式下载 ZIP（有归档则直接回源；否则即时打包，缓存缺失的页会即时补抓）
curl -OJ http://localhost:8080/api/prefill/1/zip
```

进度（`progress.done/cached/fetched`）只在任务于本进程内运行时存在，不落库；数据库仅存任务元信息、归档信息与终态。进程重启后 `queued`/`running` 的任务自动重新排队。详见 `backend/API.md`。
