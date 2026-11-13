# Quadlet 部署：Pod + Cloudflare WARP

Manga Reader 以 Podman Quadlet 部署，`manga-reader` 与 Cloudflare WARP 客户端
运行在同一个 pod 里，共享网络命名空间，出站流量经 WARP 进入 Cloudflare。

## 拓扑

```text
pod: manga-reader-pod（共享 netns，发布 127.0.0.1:5173 → 8080）
├── manga-reader-pod-infra   pod 基础容器（持有 netns）
├── manga-reader-warp        Cloudflare 官方客户端 + gost
│     ├── warp-svc           默认 warp 模式（接管 pod netns 路由）
│     ├── gost -L :1080      HTTP + SOCKS5 代理（pod 内 127.0.0.1:1080）
│     └── /var/lib/cloudflare-warp（命名卷 manga-reader-warp_data）
└── systemd-manga-reader     应用（Gin + 内嵌前端）
      ├── HTTP_PROXY/HTTPS_PROXY=socks5://127.0.0.1:1080
      ├── 数据卷 manga-reader-public_backend_data → /app/data
      └── HealthCmd: /healthz
```

出站链路：

```text
manga-reader → socks5://127.0.0.1:1080（gost）→ WARP 隧道 → Cloudflare 出口
```

使用显式代理而不是只依赖 WARP 接管路由，是为了 **fail closed**：WARP 未连上时
1080 无监听，后端请求直接失败，不会静默回退成直连。

## 文件

| 文件 | 作用 |
|---|---|
| `manga-reader.pod` | 创建 pod、发布端口、设置 WARP 需要的 netns sysctl |
| `manga-reader-warp.container` | Cloudflare WARP 客户端（`docker.io/caomingjun/warp`） |
| `manga-reader.container` | 应用容器，加入 pod，指向 pod 内 gost 代理 |

## 安装 / 升级

```bash
sudo cp deploy/quadlet/manga-reader.pod \
        deploy/quadlet/manga-reader.container \
        deploy/quadlet/manga-reader-warp.container \
        /etc/containers/systemd/

sudo systemctl daemon-reload
sudo systemctl restart manga-reader.service
```

- Quadlet 单元无法 `systemctl enable`，`manga-reader.container` 里的
  `[Install] WantedBy=multi-user.target` 由 generator 在开机时应用
  （生成 `/run/systemd/generator/multi-user.target.wants/manga-reader.service`）。
- `manga-reader.service` 会通过 `Wants=` 拉起 `manga-reader-warp.service`；
  两个容器都通过 `Pod=` 绑定到 pod。
- 端口发布必须写在 `.pod` 的 `PublishPort`，容器单元不能再写 `PublishPort`。

## 宿主前置条件

1. **rootful Podman**，且存在 `/dev/net/tun`（WARP 需要创建 tun 设备）。
2. `ufw` 若处于默认 `deny (routed)`，必须放行 pod 网桥的转发，否则 pod 完全没有
   出网，WARP 连注册 API 都会超时：

   ```bash
   ufw route allow in on podman0
   ```

   该命令会写入 `/etc/ufw/user.rules`，重启后仍生效。
3. `.env` 里的 `HTTP_PROXY` / `HTTPS_PROXY` 会被单元里的 `Environment=` 覆盖为
   `socks5://127.0.0.1:1080`；内网地址（MinIO / 数据库 / 服务名）继续放在
   `.env` 的 `NO_PROXY` 中。

## 验证

```bash
# WARP 是否连上
sudo podman exec manga-reader-warp sudo warp-cli --accept-tos status

# 代理是否真的走 Cloudflare（期望 warp=on / warp=plus）
sudo podman exec manga-reader-warp curl -fsS --socks5-hostname 127.0.0.1:1080 \
  https://cloudflare.com/cdn-cgi/trace | grep -E '^(warp|ip)='

# 应用健康
curl -fsS http://127.0.0.1:5173/healthz

# 容器状态
sudo podman ps --filter name=manga-reader
```

## 故障排查

| 现象 | 原因 / 处理 |
|---|---|
| `curl: (7) Failed to connect to 127.0.0.1 port 1080` | gost 尚未启动（entrypoint 还在注册/连接 WARP），或 WARP 容器重启中。先看 `podman logs manga-reader-warp`。 |
| pod 内 `curl http://1.1.1.1` 超时、DNS 超时 | 宿主转发被拦。检查 `ufw status verbose`，执行 `ufw route allow in on podman0`。 |
| `warp-cli status` 显示 `Unable … Registration Missing` | 出口不通导致注册失败，或卷里的注册已失效。先修好出网；仍失败时删掉卷 `manga-reader-warp_data` 让它重新注册（会触发一次新注册）。 |
| 日志出现 `open tun: operation not permitted` | 容器缺少 `/dev/net/tun` 或 `NET_ADMIN`，检查 `manga-reader-warp.container` 的 `AddDevice` / `AddCapability`。 |
| ExHentai 返回 sad panda | 与 WARP 无必然关系。先用同一份 Cookie 从宿主机直连对比；若直连也 sad panda，说明 `igneous` / `ipb_member_id` / `ipb_pass_hash` 需要从浏览器重新复制。 |

## 变更记录

### `dcb35398` feat(deploy): run manga-reader in a pod behind Cloudflare WARP

- 新增 `manga-reader.pod`：端口发布从容器移到 pod，设置
  `net.ipv6.conf.all.disable_ipv6=0`、`net.ipv4.conf.all.src_valid_mark=1`
- 新增 `manga-reader-warp.container`：WARP 客户端，挂载 tun 设备，
  授予 `NET_ADMIN` / `MKNOD` / `AUDIT_WRITE`，注册信息持久化到命名卷
- `manga-reader.container`：加入 pod、移除 `PublishPort`、
  `HTTP_PROXY/HTTPS_PROXY` 指向 pod 内 gost、`Wants/After` WARP 服务

### `3c4926d` fix(deploy): enable boot start and document pod egress firewall rule

- 为应用单元增加 `[Install] WantedBy=multi-user.target`，保证开机自启
- 在 `manga-reader.pod` 中记录宿主 `ufw route allow in on podman0` 前置条件

### 服务器侧操作（2026-11-12，tokyo-server）

1. `ufw route allow in on podman0`：修复 ufw 默认 `deny routed` 导致的容器无出网。
2. `systemctl restart manga-reader-warp.service`：在有出网的情况下重新注册并连接 WARP。
3. 冷启动验证：`manga-reader.service`、`manga-reader-warp.service`、
   `manga-reader-pod.service` 均 active，WARP `Connected`，trace 为 `warp=on`。
4. ExHentai 仍 sad panda；宿主机直连同样如此，先按 Cookie 失效处理（在应用内重新保存该账号的
   Cookie），暂不归因于 WARP 出口。
