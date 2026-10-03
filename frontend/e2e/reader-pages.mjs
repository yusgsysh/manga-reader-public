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
    const shown = await page.locator(".comimi-seek").innerText();
    check("mounts after the first batch", mountedMs < 6000, `${mountedMs}ms`);
    check("streams from the live endpoint", livePages === 1, `${livePages} requests`);
    check("shows the gallery total", /1\s*\/\s*50/.test(shown), shown);
    await page.close();
  }

  console.log("scenario 1004: cache miss, continuous stream (no batch pause)");
  {
    const { page, livePages, mountedMs } = await openReader(browser, 1004);
    check(
      "mounts from the first pages before the stream completes",
      mountedMs < 2500,
      `${mountedMs}ms`,
    );
    check("streams from the live endpoint", livePages === 1, `${livePages} requests`);
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
    const thumbRequests = [];
    page.on("request", (req) => {
      if (req.url().includes("/api/image-cache/page-thumbnail")) {
        thumbRequests.push(req.url());
      }
    });
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

    await sleep(300);
    const first = thumbRequests[0] ? new URL(thumbRequests[0]) : null;
    check(
      "thumbnails are addressed by id+token+index",
      !!first &&
        first.searchParams.get("id") === "1001" &&
        first.searchParams.get("token") === "tok" &&
        first.searchParams.has("index") &&
        !first.searchParams.has("url"),
      first ? first.search : `no requests (${thumbRequests.length})`,
    );
    await page.close();
  }

  console.log("reader: page-list thumbnails load eagerly");
  {
    // Opens the reader (optionally in fullscreen) with the page list showing
    // and returns helpers to sample the loaded thumbnail state.
    const openReader = async (fullscreen) => {
      const page = await browser.newPage();
      let thumbRequests = 0;
      page.on("request", (req) => {
        if (req.url().includes("/api/image-cache/page-thumbnail")) thumbRequests++;
      });
      await page.goto(`${BASE}/reader/1001/tok`, { waitUntil: "domcontentloaded" });
      await page.waitForSelector(".reader-shell", { timeout: 30000 });
      await sleep(1500);
      if (fullscreen) {
        // Fullscreen now lives in comimi's dock switcher; the F key emits the
        // fullscreenRequest event that ReaderPage routes to toggleFullscreen.
        await page.keyboard.press("f");
        await sleep(500);
      }
      await page.keyboard.press("m");
      await page.waitForSelector(".comimi-menu-link", { timeout: 10000 });
      await page.locator(".comimi-menu-link").first().click();
      await page.waitForSelector(".comimi-menu-view-page-list", { timeout: 10000 });
      await sleep(1500);
      const state = () =>
        page.evaluate(() => {
          // "Visible" = fully inside the page-list scroller: its overflow
          // clips the list, so viewport bounds alone would count the
          // thumbnails hidden below the scroll window too.
          const box = document
            .querySelector(".comimi-page-list-inner")
            .getBoundingClientRect();
          const win = {
            l: box.left + 4,
            r: box.right - 4,
            t: box.top + 4,
            b: box.bottom - 4,
          };
          const imgs = [
            ...document.querySelectorAll(".comimi-page-list-item img"),
          ];
          const visible = imgs.filter((img) => {
            const r = img.getBoundingClientRect();
            return (
              r.width > 0 &&
              r.left >= win.l &&
              r.right <= win.r &&
              r.top >= win.t &&
              r.bottom <= win.b
            );
          });
          const placeholder = (img) => img.src.startsWith("data:");
          return {
            layout:
              document.querySelector(".comimi-root")?.dataset.layout ?? null,
            total: imgs.length,
            visible: visible.length,
            deferred: visible.filter(placeholder).length,
            placeholders: imgs.filter(placeholder).length,
            loaded: imgs.filter((img) => !placeholder(img)).length,
          };
        });
      return { page, state, requests: () => thumbRequests };
    };

    const inline = await openReader(false);
    const inlineState = await inline.state();
    check(
      "visible thumbnails load in the inline layout",
      inlineState.visible > 0 && inlineState.deferred === 0,
      JSON.stringify(inlineState),
    );
    // Eager mode: off-screen thumbnails in the list must load too, not just
    // the ones inside the scroller.
    check(
      "every page-list thumbnail loads without scrolling",
      inlineState.total > 0 && inlineState.placeholders === 0,
      JSON.stringify(inlineState),
    );
    await inline.page.close();

    // Fullscreen makes comimi's root a viewport-fixed box, which used to
    // collapse the IntersectionObserver root bounds and leave every thumbnail
    // on the placeholder. Open the list only after entering fullscreen so the
    // check sees thumbnails that have never loaded before.
    const full = await openReader(true);
    const fullState = await full.state();
    check(
      "reader entered the fullscreen layout",
      fullState.layout === "browserFullscreen" ||
        fullState.layout === "nativeFullscreen",
      String(fullState.layout),
    );
    check(
      "visible thumbnails load in fullscreen (no stuck placeholder)",
      fullState.visible > 0 && fullState.deferred === 0,
      JSON.stringify(fullState),
    );
    check(
      "page thumbnails were requested",
      full.requests() > 0,
      `${full.requests()} requests`,
    );

    // The queue must keep feeding while the list scrolls in fullscreen.
    await full.page.evaluate(() => {
      const inner = document.querySelector(".comimi-page-list-inner");
      inner.scrollTop = inner.scrollHeight;
    });
    await sleep(1500);
    const scrolled = await full.state();
    check(
      "scrolling the page list loads more thumbnails",
      scrolled.placeholders === 0 && scrolled.loaded >= fullState.loaded,
      JSON.stringify(scrolled),
    );
    await full.page.close();
  }

  console.log("reader: menu back entry returns to the gallery");
  {
    const page = await browser.newPage();
    await page.goto(`${BASE}/reader/1001/tok`, { waitUntil: "domcontentloaded" });
    await page.waitForSelector(".reader-shell", { timeout: 30000 });
    await page.keyboard.press("m");
    await page.waitForSelector(".comimi-menu-link", { timeout: 10000 });
    const back = page.locator(".comimi-menu-link").last();
    const label = (await back.innerText()).trim();
    check(
      "menu shows the back entry below About comimi",
      label.includes("返回画廊"),
      label,
    );
    await back.click();
    await page.waitForURL(`${BASE}/gallery/1001/tok`, { timeout: 10000 });
    check(
      "back entry returns to the gallery",
      new URL(page.url()).pathname === "/gallery/1001/tok",
      page.url(),
    );
    await page.close();
  }

  console.log("bookshelf: reading round trip keeps the source nav highlighted");
  {
    const page = await browser.newPage();
    await page.goto(`${BASE}/bookshelf`, { waitUntil: "domcontentloaded" });
    await page.waitForSelector('a[href^="/gallery/1001/"]', { timeout: 30000 });

    const deadLink = await page.locator('aside a[href="/gallery"]').count();
    check("sidebar has no gallery entry", deadLink === 0, `${deadLink} links`);

    await page.locator('a[href^="/gallery/1001/"]').first().click();
    await page.waitForURL(`${BASE}/gallery/1001/tok`, { timeout: 10000 });

    await page
      .getByRole("button", { name: /^(开始阅读|继续阅读|重新阅读)/ })
      .click();
    await page.waitForSelector(".reader-shell", { timeout: 30000 });
    await page.keyboard.press("m");
    await page.waitForSelector(".comimi-menu-link", { timeout: 10000 });
    await page.locator(".comimi-menu-link").last().click();
    await page.waitForURL(`${BASE}/gallery/1001/tok`, { timeout: 10000 });
    await page.waitForSelector('aside a[href="/bookshelf"][aria-current="page"]', {
      timeout: 10000,
    });

    const activeTotal = await page.locator("aside a[aria-current='page']").count();
    const activeBookshelf = await page
      .locator('aside a[href="/bookshelf"][aria-current="page"]')
      .count();
    check(
      "detail highlights the bookshelf as the source",
      activeBookshelf === 1,
      `${activeBookshelf} active`,
    );
    check(
      "no other sidebar entry is active",
      activeTotal === 1,
      `${activeTotal} active`,
    );

    await page.getByRole("button", { name: "返回" }).click();
    await page.waitForURL(`${BASE}/bookshelf`, { timeout: 10000 });
    check(
      "detail back returns to the bookshelf",
      new URL(page.url()).pathname === "/bookshelf",
      page.url(),
    );
    const shelfActive = await page
      .locator('aside a[href="/bookshelf"][aria-current="page"]')
      .count();
    check("bookshelf highlights itself", shelfActive === 1, `${shelfActive} active`);
    await page.close();
  }

  console.log("bookshelf: reading reorders the shelf without a reload");
  {
    const page = await browser.newPage();
    // Delay bookshelf refetches after the first load so the assertion only
    // passes if the client reordered its cached shelf optimistically.
    let bookshelfRequests = 0;
    await page.route("**/api/bookshelf**", async (route) => {
      const reqUrl = new URL(route.request().url());
      if (reqUrl.pathname === "/api/bookshelf") {
        bookshelfRequests++;
        if (bookshelfRequests > 1) await sleep(5000);
      }
      try {
        await route.continue();
      } catch {
        // page closed while a delayed refetch was pending
      }
    });

    const shelfOrder = () =>
      page.$$eval(".gallery-card", (cards) =>
        cards.map((card) => {
          const href = card.querySelector('a[href^="/gallery/"]').getAttribute("href");
          return href.split("/")[2];
        }),
      );

    await page.goto(`${BASE}/bookshelf`, { waitUntil: "domcontentloaded" });
    await page.waitForSelector('.gallery-card a[href^="/gallery/1005/"]', {
      timeout: 30000,
    });
    // Mark this document: it survives SPA navigation but not a full reload.
    await page.evaluate(() => {
      window.__e2eShelfMarker = true;
    });
    check(
      "shelf starts with 1001 before 1005",
      JSON.stringify(await shelfOrder()) === JSON.stringify(["1001", "1005"]),
      JSON.stringify(await shelfOrder()),
    );

    await page.locator('a[href^="/gallery/1005/"]').first().click();
    await page.waitForURL(`${BASE}/gallery/1005/tok`, { timeout: 10000 });
    await page
      .getByRole("button", { name: /^(开始阅读|继续阅读|重新阅读)/ })
      .click();
    await page.waitForSelector(".reader-shell", { timeout: 30000 });

    // Leave the reader; the final progress save reorders the shelf.
    await page.keyboard.press("m");
    await page.waitForSelector(".comimi-menu-link", { timeout: 10000 });
    await page.locator(".comimi-menu-link").last().click();
    await page.waitForURL(`${BASE}/gallery/1005/tok`, { timeout: 10000 });

    await page.getByRole("button", { name: "返回" }).click();
    await page.waitForURL(`${BASE}/bookshelf`, { timeout: 10000 });
    await page.waitForSelector('.gallery-card a[href^="/gallery/1005/"]', {
      timeout: 10000,
    });

    check(
      "reading keeps the bookshelf session (no full reload)",
      await page.evaluate(() => window.__e2eShelfMarker === true),
    );
    check(
      "reading moves 1005 to the front of the shelf",
      JSON.stringify(await shelfOrder()) === JSON.stringify(["1005", "1001"]),
      JSON.stringify(await shelfOrder()),
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
