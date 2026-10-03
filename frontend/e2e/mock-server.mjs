// Self-contained app + API server for the reader/detail e2e tests.
//
// It serves the production build in ../dist and mocks the API endpoints the
// reader and gallery detail pages use, so the tests need no real backend or
// ExHentai access. The scenario is selected by the gallery id in the URL:
//
//   1001  complete list already cached; the live endpoint is slow (5s). With
//         the cache placeholder the reader must mount from cache immediately,
//         while still requesting the live /pages stream.
//   1002  cache miss; the live /pages stream sends meta + the first 40 pages,
//         pauses 8s, then sends the rest — the reader must mount during the
//         pause, before the stream completes.
//   1003  complete cache; every online endpoint returns 502 (simulated
//         ExHentai outage). The reader and gallery detail must render from the
//         cache, and the download button must queue from the cached pages.
//   1004  cache miss; the live /pages stream drips pages every 100ms with no
//         long pause — the reader must still mount from the first pages well
//         before the whole stream completes.
//
// Usage: E2E_PORT=4188 bun e2e/mock-server.mjs

const dist = new URL("../dist/", import.meta.url).pathname;
const PORT = Number(process.env.E2E_PORT ?? 4188);
const PAGES = 50;

const CORS = {
  "Access-Control-Allow-Origin": "*",
  "Access-Control-Allow-Methods": "GET, PUT, POST, OPTIONS",
  "Access-Control-Allow-Headers": "*",
};

// 1x1 transparent PNG.
const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMBAQDJ/pLvAAAAAElFTkSuQmCC",
  "base64",
);

const json = (body, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { ...CORS, "Content-Type": "application/json" },
  });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const isDown = (id) => id === "1003";

// Runtime state for the mocked /api/dev/upstream-down dev-tools endpoint.
let devDown = false;

function page(i) {
  return {
    page_url: `https://exhentai.org/s/tok/${i + 1}-${i + 1}`,
    index: i,
    thumbnail: {
      sprite_url: "https://cdn.hath.network/x/1-0.webp",
      x: 0,
      y: 0,
      width: 200,
      height: 282,
    },
  };
}

function fullList(id) {
  return {
    id: String(id),
    token: "tok",
    total: PAGES,
    pages: Array.from({ length: PAGES }, (_, i) => page(i)),
  };
}

// The cached /pages endpoint streams the same NDJSON as the live one.
function cachedPagesStream(id) {
  const list = fullList(id);
  let body = `${JSON.stringify({ type: "meta", id: list.id, token: list.token, total: list.total })}\n`;
  for (const p of list.pages) {
    body += `${JSON.stringify({ type: "page", ...p })}\n`;
  }
  body += `${JSON.stringify({ type: "done", total: list.total })}\n`;
  return new Response(body, {
    headers: { ...CORS, "Content-Type": "application/x-ndjson; charset=utf-8" },
  });
}

function galleryMeta(id) {
  return {
    id: Number(id),
    token: "tok",
    title: "E2E Gallery",
    title_jpn: "",
    category: "doujinshi",
    thumbnail: "",
    page_count: PAGES,
    rating: 4.5,
    rating_count: 1,
    uploader: "e2e",
    tags: [],
  };
}

function galleryDetail(id) {
  return {
    id: Number(id),
    token: "tok",
    domain: "",
    title: "E2E Gallery",
    title_jpn: "",
    cover: "",
    category: "doujinshi",
    uploader: "e2e",
    posted: "",
    parent: 0,
    visible: "",
    language: "",
    translated: false,
    file_size: "",
    page_count: PAGES,
    favorited: 0,
    rating_count: 1,
    rating: 4.5,
    tags: [],
  };
}

function bookshelfItem(id) {
  const detail = galleryDetail(id);
  return {
    id: detail.id,
    token: detail.token,
    title: detail.title,
    title_jpn: detail.title_jpn,
    category: detail.category,
    thumbnail: `https://exhentai.org/t/${detail.id}-tok.jpg`,
    pages: detail.page_count,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-02T00:00:00Z",
  };
}

function ndjsonStream(id) {
  const encoder = new TextEncoder();
  let cancelled = false;
  return new ReadableStream({
    async start(controller) {
      const send = (event) => {
        if (cancelled) return;
        try {
          controller.enqueue(encoder.encode(`${JSON.stringify(event)}\n`));
        } catch {
          // client disconnected
        }
      };
      if (id === "1001") await sleep(5000);
      send({ type: "meta", id: String(id), token: "tok", total: PAGES });
      for (let i = 0; i < PAGES; i++) {
        if (cancelled) return;
        send({ type: "page", ...page(i) });
        if (id === "1002" && i === 39) await sleep(8000);
        if (id === "1004") await sleep(100);
      }
      if (!cancelled) {
        send({ type: "done", total: PAGES });
        try {
          controller.close();
        } catch {
          // already closed
        }
      }
    },
    cancel() {
      cancelled = true;
    },
  });
}

const server = Bun.serve({
  port: PORT,
  idleTimeout: 60,
  async fetch(req) {
    const url = new URL(req.url);
    const path = url.pathname;

    if (req.method === "OPTIONS") {
      return new Response(null, { status: 204, headers: CORS });
    }
    if (path === "/healthz") {
      return new Response("ok", { headers: CORS });
    }

    // Mocked dev-tools switch (the settings page toggles this).
    if (path === "/api/dev/upstream-down") {
      if (req.method === "PUT") {
        const body = await req.json().catch(() => ({}));
        devDown = !!body.down;
      }
      return json({ down: devDown });
    }

    // ---- Cached (offline fallback) endpoints -----------------------------
    const noCache = (id) => id === "1002" || id === "1004";
    let m = path.match(/^\/api\/gallery-cache\/(\d+)\/tok\/pages$/);
    if (m) {
      if (noCache(m[1])) return json({ error: "no cached pages" }, 404);
      return cachedPagesStream(m[1]);
    }
    m = path.match(/^\/api\/gallery-cache\/(\d+)\/tok\/details$/);
    if (m) {
      if (noCache(m[1])) return json({ error: "no cached details" }, 404);
      return json(galleryDetail(m[1]));
    }
    m = path.match(/^\/api\/gallery-cache\/(\d+)\/tok$/);
    if (m) {
      if (noCache(m[1])) return json({ error: "no cached gallery" }, 404);
      return json(galleryMeta(m[1]));
    }

    // ---- Live (upstream) endpoints ---------------------------------------
    m = path.match(/^\/api\/gallery\/(\d+)\/tok\/pages$/);
    if (m) {
      if (isDown(m[1])) return json({ error: "simulated upstream outage" }, 502);
      return new Response(ndjsonStream(m[1]), {
        headers: { ...CORS, "Content-Type": "application/x-ndjson; charset=utf-8" },
      });
    }
    m = path.match(/^\/api\/gallery\/(\d+)\/tok\/details$/);
    if (m) {
      if (isDown(m[1])) return json({ error: "simulated upstream outage" }, 502);
      return json(galleryDetail(m[1]));
    }
    m = path.match(/^\/api\/gallery\/(\d+)\/tok$/);
    if (m) {
      if (isDown(m[1])) return json({ error: "simulated upstream outage" }, 502);
      return json(galleryMeta(m[1]));
    }

    // ---- Local database endpoints ----------------------------------------
    if (path === "/api/bookshelf") {
      const page = Number(url.searchParams.get("page") ?? "0") || 0;
      return json({
        page,
        page_size: 24,
        total: 1,
        total_pages: 1,
        results: [bookshelfItem(1001)],
      });
    }
    m = path.match(/^\/api\/bookshelf\/(\d+)\/tok\/status$/);
    if (m) return json({ in_bookshelf: false });
    if (path.startsWith("/api/bookshelf")) return json({ success: true });

    if (path.startsWith("/api/progress/")) {
      return json({
        gallery_id: 1,
        token: "tok",
        current_page: 0,
        progress: 0,
        completed: false,
        created_at: null,
        updated_at: null,
      });
    }

    if (path === "/api/prefill" && req.method === "POST") {
      const body = await req.json().catch(() => ({}));
      return json({
        id: 1,
        status: "queued",
        total: Array.isArray(body.urls) ? body.urls.length : 0,
      });
    }
    if (path.startsWith("/api/prefill")) return json({ jobs: [] });

    // ---- Torrents (live, no cache) ---------------------------------------
    let t = path.match(/^\/api\/gallery\/(\d+)\/tok\/torrents$/);
    if (t) {
      return json({
        torrents: [
          {
            gtid: "2250081",
            name: "E2E Gallery [e2e].zip",
            size: "42.13 MiB",
            posted: "2026-10-02 06:30",
            seeds: 8,
            peers: 4,
            downloads: 12,
            uploader: "e2e",
          },
        ],
      });
    }
    t = path.match(/^\/api\/gallery\/(\d+)\/tok\/torrents\/(\d+)\/info$/);
    if (t) {
      return json({
        posted: "2026-10-02 06:30",
        seeds: 9,
        uploader: "e2e",
        dlers: 5,
        size: "42.13 MiB",
        completes: 12,
        comments: "No comments were given for this torrent.",
        personalized: true,
      });
    }
    t = path.match(/^\/api\/gallery\/(\d+)\/tok\/torrents\/(\d+)\/download$/);
    if (t) {
      return new Response("torrent-bytes", {
        headers: { ...CORS, "Content-Type": "application/x-bittorrent" },
      });
    }

    if (path.startsWith("/api/image-cache/") || path.startsWith("/api/image/")) {
      return new Response(PNG, { headers: { ...CORS, "Content-Type": "image/png" } });
    }

    if (path.startsWith("/api/")) {
      return json({ error: `unhandled ${path}` }, 404);
    }

    // ---- Static assets from dist (SPA fallback) --------------------------
    const relative = path === "/" ? "index.html" : path.replace(/^\//, "");
    const file = Bun.file(dist + relative);
    if (await file.exists()) {
      return new Response(file);
    }
    return new Response(Bun.file(`${dist}index.html`), {
      headers: { "Content-Type": "text/html" },
    });
  },
});

console.log(`e2e mock server listening on http://127.0.0.1:${server.port}`);
