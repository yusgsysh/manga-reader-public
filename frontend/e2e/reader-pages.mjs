// Reader / gallery-detail loading e2e tests.
//
// Verifies the frontend-only "online-first, cache placeholder + fallback"
// behaviour:
//   1001  complete cache, slow live endpoint -> mounts from cache immediately,
//         while still requesting the live stream;
//   1002  cache miss -> streams progressively, mounts after the first batch
//         before the stream completes;
//   1003  complete cache, upstream down (all online endpoints 502) -> the
//         reader and gallery detail render from cache, and download queues the
//         cached page urls.
//
// Requires a production build (bun run build:e2e) and Chromium
// (bun run test:e2e:install). Run with: bun run test:e2e

import { spawn } from "node:child_process";
import { setTimeout as sleep } from "node:timers/promises";
import { chromium } from "playwright";

const PORT = Number(process.env.E2E_PORT ?? 4188);
const BASE = `http://127.0.0.1:${PORT}`;

// 1x1 transparent PNG, used to fulfil delayed thumbnail requests.
const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMBAQDJ/pLvAAAAAElFTkSuQmCC",
  "base64",
);

let failures = 0;
function check(name, condition, detail) {
  if (condition) {
    console.log(`  ok   ${name}`);
  } else {
    failures++;
    console.error(`  FAIL ${name}${detail ? ` — ${detail}` : ""}`);
  }
}

const server = spawn("bun", ["e2e/mock-server.mjs"], {
  cwd: new URL("..", import.meta.url).pathname,
  env: { ...process.env, E2E_PORT: String(PORT) },
  stdio: "inherit",
});

async function waitForServer() {
  for (let i = 0; i < 100; i++) {
    try {
      const res = await fetch(`${BASE}/healthz`);
      if (res.ok) return;
    } catch {
      // not up yet
    }
    await sleep(100);
  }
  throw new Error("mock server did not start");
}

async function openReader(browser, id) {
  const page = await browser.newPage();
  let livePages = 0;
  page.on("request", (req) => {
    if (/\/api\/gallery\/\d+\/tok\/pages$/.test(req.url())) livePages++;
  });
  const started = Date.now();
  await page.goto(`${BASE}/reader/${id}/tok`, { waitUntil: "domcontentloaded" });
  await page.waitForSelector(".reader-shell", { timeout: 30000 });
  return { page, livePages, mountedMs: Date.now() - started };
}

try {
  await waitForServer();
  const browser = await chromium.launch();

  console.log("scenario 1001: cache present, slow live endpoint");
  {
    const { page, livePages, mountedMs } = await openReader(browser, 1001);
    check("mounts from cache before the slow live stream", mountedMs < 3000, `${mountedMs}ms`);
    check("still requests the live stream", livePages === 1, `${livePages} requests`);
    await page.close();
  }

  console.log("scenario 1002: cache miss, progressive stream");
  {
    const { page, livePages, mountedMs } = await openReader(browser, 1002);
    const shown = await page.locator(".reader-topbar").innerText();
    check("mounts after the first batch", mountedMs < 6000, `${mountedMs}ms`);
    check("streams from the live endpoint", livePages === 1, `${livePages} requests`);
    check("shows the gallery total", /1\s*\/\s*50/.test(shown), shown);
    await page.close();
  }

  console.log("scenario 1003: cache present, upstream down");
  {
    const { page, livePages, mountedMs } = await openReader(browser, 1003);
    check("reader opens from cache while upstream is down", mountedMs < 5000, `${mountedMs}ms`);
    check("live stream was attempted", livePages === 1, `${livePages} requests`);
    await page.close();
  }

  console.log("scenario 1003: gallery detail + download from cache");
  {
    const page = await browser.newPage();
    const prefilled = page.waitForRequest(
      (req) => req.url().endsWith("/api/prefill") && req.method() === "POST",
      { timeout: 30000 },
    );
    await page.goto(`${BASE}/gallery/1003/tok`, { waitUntil: "domcontentloaded" });
    await page.waitForSelector("h1", { timeout: 30000 });

    const errorBlocks = await page.locator("text=无法加载 Gallery").count();
    check("detail renders from cache (no error state)", errorBlocks === 0, `${errorBlocks} error blocks`);

    await page.locator('button[aria-label="添加下载任务"]').click();
    const body = (await prefilled).postDataJSON();
    check(
      "download queues the cached page urls",
      Array.isArray(body?.urls) && body.urls.length === 50,
      `urls=${body?.urls?.length}`,
    );
    await page.close();
  }

  console.log("gallery thumbnails: loading shimmer");
  {
    const page = await browser.newPage();
    // Delay the page thumbnails so the loading shimmer stays observable.
    await page.route("**/api/image-cache/page-thumbnail**", async (route) => {
      await sleep(1500);
      await route.fulfill({ status: 200, contentType: "image/png", body: PNG });
    });
    await page.goto(`${BASE}/gallery/1001/tok`, { waitUntil: "domcontentloaded" });
    await page.waitForSelector(".page-thumb-item", { timeout: 30000 });
    const box = await page.evaluate(() => {
      const el = document.querySelector(".page-thumb-item .skeleton-shimmer");
      if (!el) return null;
      const r = el.getBoundingClientRect();
      return { w: Math.round(r.width), h: Math.round(r.height) };
    });
    check(
      "page thumbnail shimmer is visible while loading",
      !!box && box.w > 0 && box.h > 0,
      JSON.stringify(box),
    );
    await page.close();
  }

  console.log("settings: dev tools section");
  {
    // Enabled backend: the switch is shown.
    const page = await browser.newPage();
    await page.goto(`${BASE}/settings`, { waitUntil: "domcontentloaded" });
    await page.waitForSelector("text=调试工具", { timeout: 30000 });
    await page.waitForSelector("text=模拟 ExHentai 不可用", { timeout: 30000 });
    const hint = await page.getByText("调试接口未开启").count();
    check("shows the toggle when dev tools are enabled", hint === 0, `${hint} hint`);

    const toggled = page.waitForRequest(
      (req) => req.url().endsWith("/api/dev/upstream-down") && req.method() === "PUT",
      { timeout: 10000 },
    );
    await page.getByRole("switch").click();
    const body = (await toggled).postDataJSON();
    check("toggle requests the outage switch", body?.down === true, JSON.stringify(body));
    await page.close();
  }
  {
    // Disabled backend: the endpoint 404s, so the section shows a hint.
    const page = await browser.newPage();
    await page.route("**/api/dev/upstream-down", (route) =>
      route.fulfill({ status: 404, contentType: "application/json", body: '{"error":"not found"}' }),
    );
    await page.goto(`${BASE}/settings`, { waitUntil: "domcontentloaded" });
    await page.waitForSelector("text=调试接口未开启", { timeout: 30000 });
    const toggle = await page.getByText("模拟 ExHentai 不可用").count();
    check("shows a not-enabled hint when dev tools are off", toggle === 0, `${toggle} toggle`);
    await page.close();
  }

  await browser.close();
} catch (error) {
  failures++;
  console.error("e2e error:", error);
} finally {
  server.kill("SIGTERM");
}

if (failures > 0) {
  console.error(`\n${failures} e2e check(s) failed`);
  process.exit(1);
}
console.log("\nall e2e checks passed");
