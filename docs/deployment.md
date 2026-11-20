# 公网部署（HTTPS + 三类凭证）

认证按接口用途拆分，凭证互不通用：

| 接口类型 | 认证机制 |
|----------|----------|
| 用户业务 API（登录、书架、进度、Prefill、个人设置、用户管理） | `Authorization: Bearer <JWT>` |
| 同步 API（`/api/sync/push`、`/api/sync/events`） | `Authorization: Bearer <用户级同步 Key>` |
| Metrics 内部接口 | 回环限制，或独立 `MANGA_READER_METRICS_TOKEN` |
| Health / Readiness | 最小信息公开：`/healthz`、`/readyz` |

- **HTTPS 必须在认证之前**：JWT 的 HttpOnly Cookie 和同步 Key 都必须走 TLS。TLS 由前置层（云负载均衡 / Caddy / 前置 nginx）提供，同时负责 HTTP :80 → HTTPS :443 跳转；HSTS 也由该层配置。
- **应用端口不暴露公网**：容器 `:8080`、PostgreSQL `:5432`、MinIO `:9000/:9001` 不发布到公网；compose 与 quadlet 默认发布到 `127.0.0.1:5173:8080`，公网只经过带 TLS 的前置反代。确实需要局域网直连时才改成 `0.0.0.0:5173:8080`。
- **Basic Auth 已完全移除**：后端、前端、compose/quadlet、observability 和文档都不再接受或配置 HTTP Basic Authentication。

## JWT 登录认证与多用户

多用户模式始终启用。`JWT_SECRET` 可留空：后端在首次启动时自动生成一个 32 字节随机密钥并存入数据库（`app_setting` 表），之后重启复用同一密钥，会话与加密 Cookie 不受影响，无需手动设置。需要固定密钥（如多实例共享同一数据库）时显式设置随机值（至少 32 字节），环境变量优先于数据库：

```bash
openssl rand -hex 32
```

```env
JWT_SECRET=<可选，粘贴上一步输出>
JWT_ISSUER=manga-reader
JWT_EXPIRES_IN=1h
ADMIN_USERNAME=admin
ADMIN_PASSWORD=change-me-strong-password   # 可选；留空则通过 Web 端 /setup 页面初始化管理员
MANGA_READER_AUTH_COOKIE_SECURE=auto
```

> 密钥解析顺序：`JWT_SECRET` 环境变量 → 数据库 `app_setting`（键 `auth.jwt_secret`）→ 首次启动生成并写库。清空环境变量后回落到数据库中的密钥，不会重新生成。

- **首个管理员**：两种方式。① 首次启动时通过 Web 端「创建管理员账户」页面（`/setup`，`POST /api/auth/register`）创建，仅在 users 表为空时可用，创建后自动登录；② 设置 `ADMIN_USERNAME` + `ADMIN_PASSWORD`，后端在 users 表为空时创建。密码以 Argon2id PHC 哈希写入数据库，之后启动**不会覆盖**已有密码，可清空 `ADMIN_PASSWORD`。
- **忘记管理员密码**：不要通过改 `.env` 的 `ADMIN_PASSWORD` 来重置。镜像内置 `reset-admin`（密码从 stdin 读取，不出现在进程列表）：

  ```bash
  printf '%s\n' 'new-strong-password' | \
    docker compose exec -T backend /app/reset-admin -username admin
  ```

  从源码运行：

  ```bash
  cd backend
  printf '%s\n' 'new-strong-password' | go run ./cmd/reset-admin -username admin
  ```

  命令会递增 `token_version` 撤销旧 Token、把用户状态恢复为 `active`，并**清除该账号的 TOTP 密钥与恢复码**。因此即使验证器和恢复码全部丢失，重置后也可以直接用新密码登录。
- **登录接口**：`POST /api/auth/login` 只需要 `username` + `password`。成功后返回 `access_token` / `token_type` / `expires_in` 以及当前用户 `user`，同时写入 `HttpOnly; SameSite=Lax; Secure(auto)` Cookie；JWT 的 `sub` 与 `user_id` 都是当前用户 ID。前端不把 Token 放进 `localStorage`，页面刷新后由 Cookie + `GET /api/auth/status` 恢复登录态。
- **通行密钥（Passkeys / WebAuthn）**：登录页提供「使用通行密钥登录」，设置 → 通行密钥可添加、重命名、删除（每账号最多 20 个）。服务端只保存公钥与签名计数器，私钥永不离开设备；登录使用 discoverable credential，无需先输入用户名。浏览器要求安全上下文，即 HTTPS 或 `localhost`。RP 默认按请求 `Origin`/`Host` 推导；反向代理改写 Host 时设置：

  ```env
  MANGA_READER_WEBAUTHN_RP_ID=manga.example.com
  MANGA_READER_WEBAUTHN_RP_NAME=Manga Reader
  MANGA_READER_WEBAUTHN_RP_ORIGINS=https://manga.example.com
  ```

  通行密钥与密码并存：删除全部通行密钥后仍可用密码登录。
- **TOTP 两步验证**：设置 → 两步验证可扫码绑定 Google Authenticator、1Password、Bitwarden 等应用。启用后 `POST /api/auth/login` 只返回短期的 `mfa_token`（5 分钟，独立 issuer，不能当作访问令牌），前端再调用 `POST /api/auth/totp/verify` 提交 6 位验证码或一个恢复码，成功后才会签发正常 JWT/Cookie。启用时生成 10 个一次性恢复码（仅显示一次、只存 HMAC 哈希）；关闭或重新生成恢复码需要密码 + 当前验证码。TOTP 密钥由 `JWT_SECRET` 派生密钥加密存储，更换 `JWT_SECRET` 会导致已绑定验证器失效。**通行密钥登录不受 TOTP 影响**（通行密钥本身已是抗钓鱼的多因素认证）。
- **角色**：`admin` 管理实例级用户和系统同步配置；`user` 只能使用业务功能和个人设置。普通用户的 JWT 调用 `/api/admin/*`、`/api/sync/config`、`/api/sync/status`、`/api/dev/*` 会得到 403。
- **数据隔离**：`bookshelf`、`reading_progress`、`prefill_job`、`tag_preference`、`tag_sync_baseline`、`sync_change`、`passkey` 全部带 `user_id`，所有查询/创建/更新/删除都从 JWT 取用户 ID 并附加谓词。客户端传入的 `user_id`（路径、查询、Header、Body）一律被忽略。`gallery_cache`、缩略图等公共上游元数据继续全局共享。
- **Cookie 与个人设置**：`user_setting` 每个用户一行，`cookie_ciphertext` 使用由 `JWT_SECRET` 派生的 AES-256-GCM 密钥加密；接口只返回是否配置、验证状态、站点等非敏感摘要。每个用户使用独立 HTTP Client / Cookie Jar（会话 Cookie 同时写入 exhentai.org 与 e-hentai.org 两个域），日志只记录长度不记录值。每用户可选浏览站点（`e-hentai` 默认 / `exhentai`）：默认使用 e-hentai.org 且 Cookie 可为空，未配置 Cookie 时匿名浏览公开画廊；选择 exhentai 时必须配置 Cookie。搜索、列表、画廊页与种子页按所选站点抓取（两站 gallery id/token 通用）；标签同步固定走 exhentai（`s.exhentai.org` 变更 API 无 e-hentai 对应端点）。
- **系统级接口**：`/api/sync/push`、`/api/sync/events` 只接受用户级同步 Key；Metrics 接受独立 Token；用户 JWT 不能调用这些系统接口。
- **主键为 UUID（非自增）**：`users`、`bookshelf`、`reading_progress`、`user_setting`、`user_sync_key`、`prefill_job`、`passkey` 的主键都是 UUID（UUIDv7，SQLite 以 16 字节 BLOB 存储、PostgreSQL 以 bytea 存储），`gallery_cache` / `tag_preference` / `tag_sync_baseline` / `app_setting` / `sync_state` 使用自然复合键或字符串键。自增整数主键只在 `sync_change` 保留（其 `id` 同时是复制游标，需要单调递增）。
- **CSRF**：Cookie 仅用于同源请求；`csrfGuard` 会拒绝跨站 `Origin` / `Sec-Fetch-Site` 的写请求。反向代理必须透传 `Origin` / `Sec-Fetch-Site`，并保持应用与前端同源；HTTPS 终止在反代时还要设置 `X-Forwarded-Proto: https`，否则 `auto` 模式不会给 Cookie 加 `Secure`。
- **登出**：`POST /api/auth/logout` 会递增 `users.token_version` 并清除 Cookie，所有已签发 Token 立即失效。
- **Token 校验**：只接受 `Authorization: Bearer <JWT>`（或同源 Cookie），严格校验 HS256 签名、`iss`、`iat`、`exp`、`user_id` 与 `token_version`；缺失/无效/过期/签名错误统一返回 `401`。
- **公开 API**：只有 `/api/auth/login`、`/api/auth/register`、`/api/auth/status`、`/api/auth/passkey/login/*`、`/api/auth/totp/verify` 和同步端点无需用户 JWT。搜索、画廊、图片代理（`<img>`/EventSource 会携带同源 HttpOnly Cookie）也需要登录，因为它们必须使用当前用户的 ExHentai Cookie。
- **管理 API**：`/api/admin/users*` 仅管理员可用；用户只能修改自己的数据。

## 用户级同步 Key

- **一把 Key 一个账号**：在服务端「设置 → API Key」为当前账号创建（`/api/sync/keys`），可设置名称与有效期（7/30/60/90/180/365 天、自定义日期或永不过期）；数据库只保存 SHA-256 哈希和显示前缀，完整明文只在创建响应中出现一次。
- **归属固定**：持有该 Key 的对端推送的私有数据全部归属到 Key 的账号；拉取时也只返回该账号的私有数据（`gallery_cache` 为公共元数据，双方共享）。两端不需要相同用户名或相同用户 UUID。
- **删除 / 轮换**：Key 可随时删除（立即失效）或轮换（生成同名新 Key 并删除旧 Key）；同步请求记录 `request_id` 审计日志，数据合并按行 LWW 幂等。
- **客户端配置**：桌面端设置页填写服务器地址 + Key；请求使用 `Authorization: Bearer <Key>`，配置绑定保存它的当前账号。用户 JWT 不能访问同步端点。

## Metrics 与健康检查

- `/metrics` 默认启用；**未配置凭证时仅允许回环地址访问**。跨主机抓取需配置独立 Token：

  ```env
  MANGA_READER_METRICS_ENABLED=true
  MANGA_READER_METRICS_TOKEN=<openssl rand -hex 32>
  ```

  请求头使用 `Authorization: Bearer <METRICS_TOKEN>` 或 `X-Metrics-Token: <METRICS_TOKEN>`。该 Token 与 JWT、同步 Key 独立；配置后即使回环请求也必须提供 Token。用户 JWT 始终被拒绝。

- `/healthz` 是存活性检查，只返回 `{"status":"ok"}`；`/readyz` 检查数据库连接，只返回 `{"status":"ready"}` 或 `503 {"status":"unavailable"}`，不暴露 DSN、路径或密钥。

## 验证

```bash
cd backend
go test ./internal/auth/ ./internal/synckey/ ./internal/metrics/ ./internal/sync/ ./internal/web/ ./internal/httpx/ ./internal/config/
cd ../frontend && bun run test
```

覆盖：JWT 登录/失效/篡改、用户级同步 Key 认证/删除/轮换/过期/禁用、同步按用户归属与隔离、Metrics 回环与独立 Token、跨用户书架/进度/Prefill/标签/设置/Cookie 隔离、管理员权限。
