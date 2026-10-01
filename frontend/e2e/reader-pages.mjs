// Reader page-loading e2e tests.
//
// Verifies the cache-first / streaming behaviour that makes the reader open
// without waiting for the whole /pages stream:
//   1. a gallery with a complete cached list mounts from cache, ignoring a
//      slow live endpoint (no live /pages request at all);
//   2. a gallery with no cache streams progressively and mounts after the
//      first batch, before the stream completes.
//
// Requires a production build (bun run build:e2e) and Chromium
// (bun run test:e2e:install). Run with: bun run test:e2e / bun run test:e2e:install

import { spawn } from "node:child_process";
import { setTimeout as sleep } from "node:timers/promises";
import { chromium } from "playwright";

const PORT = Number(process.env.E2E_PORT ?? 4188);
const BASE = `http://127.0.0.1:${PORT}`;

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

async function loadReader(browser, id, token) {
  const page = await browser.newPage();
  let liveRequests = 0;
  page.on("request", (req) => {
    if (new RegExp(`/api/gallery/${id}/${token}/pages$`).test(req.url())) {
      liveRequests++;
    }
  });

  const started = Date.now();
  await page.goto(`${BASE}/reader/${id}/${token}`, {
    waitUntil: "domcontentloaded",
  });
  await page.waitForSelector(".reader-shell", { timeout: 30000 });
  const mountedMs = Date.now() - started;
  const shown = await page.locator(".reader-topbar").innerText();
  await page.close();
  return { mountedMs, liveRequests, shown };
}

try {
  await waitForServer();
  const browser = await chromium.launch();

  console.log("scenario 1001: complete cache, slow live endpoint");
  {
    const { mountedMs, liveRequests } = await loadReader(browser, 1001, "tok");
    check("mounts from cache quickly", mountedMs < 5000, `${mountedMs}ms`);
    check("never requests the live stream", liveRequests === 0, `${liveRequests} requests`);
  }

  console.log("scenario 1002: cache miss, progressive stream");
  {
    const { mountedMs, liveRequests, shown } = await loadReader(browser, 1002, "tok");
    // The live stream pauses 8s after the first 40 pages; mounting must happen
    // during that pause, i.e. well before the stream (and its 8s hold) ends.
    check("mounts after the first batch", mountedMs < 6000, `${mountedMs}ms`);
    check("streams from the live endpoint", liveRequests === 1, `${liveRequests} requests`);
    check("shows the first page of the gallery total", /1\s*\/\s*50/.test(shown), shown);
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
