// Reproduction / regression probe for the reader's missing loading rabbit.
//
// Symptom: when a manga page image has not loaded yet, the reader sometimes
// shows a plain white screen instead of comimi's rabbit loading animation.
//
// Root cause (in @yui540/comimi@0.26.0):
//   PageStage.buildSlot has two paths (dist/index.js, ~line 1809):
//     - normal: append `.comimi-loading-icon` (the rabbit), hide the <img>,
//       remove the icon on `load`;
//     - fast path: if `imageSources` already holds the resolved src, it just
//       does `img.src = url` with no loading icon and no hidden image.
//   `PageStage.preloadImages` pre-fills `imageSources` for +-4 pages around the
//   visible one, so turning to a preloaded page hits the fast path: until the
//   image decodes/loads, the page shows the white background with no rabbit.
//
// This script delays `/api/image-cache/page?`, opens the reader (rabbit shown on
// the first page), jumps to a preloaded page, and samples the DOM to prove the
// white-screen condition.
//
// Usage:
//   bun run repro:reader-loading            # exits 0 when the bug reproduces
//   EXPECT_FIXED=1 bun e2e/repro-reader-loading.mjs
//                                           # regression gate: exits 0 only
//                                           # once the rabbit is shown
//
// Requires a production e2e build (bun run build:e2e) and Chromium
// (bun run test:e2e:install).

import { spawn } from "node:child_process";
import { setTimeout as sleep } from "node:timers/promises";
import { chromium } from "playwright";

const PORT = Number(process.env.E2E_PORT ?? 4199);
const BASE = `http://127.0.0.1:${PORT}`;
const EXPECT_FIXED = process.env.EXPECT_FIXED === "1";
const IMAGE_DELAY_MS = Number(process.env.IMAGE_DELAY_MS ?? 5000);
// The first page's update preloads pages [current - 4, current + 4].
const PRELOAD_TARGET = Number(process.env.PRELOAD_TARGET ?? 4);

// 1x1 transparent PNG, used to fulfil the delayed page-image requests.
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

async function snapshot(page, label) {
  const data = await page.evaluate(() => {
    const group = document.querySelector(
      '.comimi-page-group[data-placement="current"]',
    );
    const slot = group?.querySelector(".comimi-page");
    const img = slot?.querySelector("img");
    return {
      loadingIcons: document.querySelectorAll(".comimi-loading-icon").length,
      splash: !!document.querySelector(".comimi-splash"),
      currentPageIndex: slot?.dataset.pageIndex ?? null,
      slotLoadingIcon: !!slot?.querySelector(".comimi-loading-icon"),
      img: img
        ? {
            complete: img.complete,
            naturalWidth: img.naturalWidth,
            visibility: getComputedStyle(img).visibility,
          }
        : null,
      topbar: document
        .querySelector(".reader-topbar")
        ?.innerText?.replace(/\s+/g, " ")
        .trim(),
    };
  });
  console.log(`  ${label} ${JSON.stringify(data)}`);
  return data;
}

async function jumpToPage(page, target) {
  await page.evaluate((value) => {
    const input = document.querySelector(".comimi-seek-input");
    if (!input) throw new Error("comimi seek input not found");
    input.value = String(value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  }, target);
  await page
    .waitForFunction(
      (value) => {
        const group = document.querySelector(
          '.comimi-page-group[data-placement="current"]',
        );
        const slot = group?.querySelector(".comimi-page");
        return slot?.dataset.pageIndex === String(value);
      },
      target,
      { timeout: 5000 },
    )
    .catch(() => {});
}

try {
  await waitForServer();
  const browser = await chromium.launch();
  const page = await browser.newPage();

  // Delay the full-size page images so the loading state stays observable.
  await page.route(/\/api\/image-cache\/page\?/, async (route) => {
    await sleep(IMAGE_DELAY_MS);
    await route.fulfill({ status: 200, contentType: "image/png", body: PNG });
  });

  await page.goto(`${BASE}/reader/1001/tok`, { waitUntil: "domcontentloaded" });
  await page.waitForSelector(".comimi-stage", { timeout: 30000 });
  await page.waitForFunction(() => !document.querySelector(".comimi-splash"), null, {
    timeout: 15000,
  });

  // A: the first page goes through the normal path -> rabbit + hidden image.
  const first = await snapshot(page, "A page0 after splash");
  check(
    "first page shows the loading rabbit",
    first.loadingIcons > 0,
    `loadingIcons=${first.loadingIcons}`,
  );
  check(
    "first page image is hidden while loading",
    first.img?.visibility === "hidden",
    JSON.stringify(first.img),
  );

  // B/C: jumping to a preloaded page hits the fast path.
  await jumpToPage(page, PRELOAD_TARGET);
  await sleep(300);
  const preloaded = await snapshot(
    page,
    `B page ${PRELOAD_TARGET + 1} +300ms (preloaded)`,
  );
  check(
    "jumped to the preloaded page",
    Number(preloaded.currentPageIndex) === PRELOAD_TARGET,
    `pageIndex=${preloaded.currentPageIndex}`,
  );
  check(
    "preloaded page image is still loading",
    preloaded.img != null && !preloaded.img.complete,
    JSON.stringify(preloaded.img),
  );

  const hitFastPath =
    preloaded.img?.visibility === "visible" &&
    !preloaded.img.complete &&
    preloaded.slotLoadingIcon === false;
  const rabbitShown =
    preloaded.slotLoadingIcon === true || preloaded.img?.visibility === "hidden";

  if (EXPECT_FIXED) {
    check(
      "preloaded page shows the rabbit while loading",
      rabbitShown,
      JSON.stringify(preloaded),
    );
  } else if (hitFastPath) {
    console.log("  ok   reproduced: preloaded page is white with no rabbit");
  } else {
    failures++;
    console.error(
      `  FAIL could not reproduce the white-screen bug — ${JSON.stringify(preloaded)}`,
    );
  }

  await sleep(IMAGE_DELAY_MS);
  await snapshot(page, `C page ${PRELOAD_TARGET + 1} after image delay`);

  await browser.close();
} catch (error) {
  failures++;
  console.error("repro error:", error);
} finally {
  server.kill("SIGTERM");
}

if (failures > 0) {
  console.error(`\n${failures} check(s) failed`);
  process.exit(1);
}
if (EXPECT_FIXED) {
  console.log("\npreloaded pages show the loading rabbit");
} else {
  console.log("\nreproduced the missing-rabbit white screen");
}
