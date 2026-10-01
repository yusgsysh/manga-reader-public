// Self-contained app + API server for the reader e2e tests.
//
// It serves the production build in ../dist and mocks the handful of API
// endpoints the reader uses, so the tests need no real backend or ExHentai
// access. The scenario is selected by the gallery id in the reader URL:
//
//   1001  complete list already cached; the live /pages endpoint is slow
//         (20s) and must NOT be requested
//   1002  cache miss; the live /pages stream sends meta + the first 40 pages,
//         pauses 8s, then sends the rest — the reader must mount during the
//         pause, before the stream completes
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

function page(i) {
  return { page_url: `https://exhentai.org/s/tok/${i + 1}-${i + 1}`, index: i };
}

function fullList(id) {
  return {
    id: String(id),
    token: "tok",
    total: PAGES,
    pages: Array.from({ length: PAGES }, (_, i) => page(i)),
  };
}

function ndjsonStream(id) {
  const encoder = new TextEncoder();
  return new ReadableStream({
    async start(controller) {
      const send = (event) => controller.enqueue(encoder.encode(`${JSON.stringify(event)}\n`));
      // 1001 must never be reached; if the cache-first logic regresses, hold the
      // response so the test fails on timing and on the live-request counter.
      if (id === "1001") {
        await sleep(20000);
      }
      send({ type: "meta", id: String(id), token: "tok", total: PAGES });
      for (let i = 0; i < PAGES; i++) {
        send({ type: "page", ...page(i) });
        if (id === "1002" && i === 39) {
          await sleep(8000);
        }
      }
      send({ type: "done", total: PAGES });
      controller.close();
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

    // Cached page list. Only 1001 has a complete cached list.
    const cacheMatch = path.match(/^\/api\/gallery-cache\/(\d+)\/tok\/pages$/);
    if (cacheMatch) {
      const id = cacheMatch[1];
      if (id === "1001") {
        return json(fullList(id));
      }
      return json({ error: "not found" }, 404);
    }

    // Streaming page list.
    const pagesMatch = path.match(/^\/api\/gallery\/(\d+)\/tok\/pages$/);
    if (pagesMatch) {
      return new Response(ndjsonStream(pagesMatch[1]), {
        headers: { ...CORS, "Content-Type": "application/x-ndjson; charset=utf-8" },
      });
    }

    const galleryMatch = path.match(/^\/api\/gallery\/(\d+)\/tok$/);
    if (galleryMatch) {
      return json({
        id: Number(galleryMatch[1]),
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
      });
    }

    if (path.startsWith("/api/progress/")) {
      return json({
        gallery_id: 1,
        token: "tok",
        current_page: 0,
        progress: 0,
        completed: false,
        started_at: null,
        updated_at: null,
      });
    }

    if (path.startsWith("/api/image-cache/") || path.startsWith("/api/image/")) {
      return new Response(PNG, { headers: { ...CORS, "Content-Type": "image/png" } });
    }

    if (path.startsWith("/api/")) {
      return json({ error: `unhandled ${path}` }, 404);
    }

    // Static assets from dist, falling back to the SPA shell.
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
