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

## 路由

| 路径 | 页面 |
|------|------|
| `/` | 首页 |
| `/watched` | 订阅 |
| `/popular` | 热门 |
| `/search` | 搜索 |
| `/bookshelf` | 书架 |
| `/gallery/:id/:token` | Gallery Detail (预留) |
