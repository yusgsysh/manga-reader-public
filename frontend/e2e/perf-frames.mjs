// Animation frame-drop benchmark for the list pages.
//
// Boots the production build with the API mocked (e2e/mock-server.mjs), runs
// Chromium with CPU throttling, and records main-thread long tasks (>50ms)
// plus frame gaps during the animations that matter:
//
//   open-early  navigation start -> first grid paint (route transition)
//   open-idle   first paint -> +5s (deferred tag-database load window)
//   scroll      wheel-scrolling the home list (infinite scroll + images)
//   route       SPA navigation home -> popular -> home (PageTransition)
//   drawer      mobile header drawer open/close
//
// Usage: bun run build:e2e && bun e2e/perf-frames.mjs
// Env:   E2E_PORT (4189), PERF_CPU_RATE (4), PERF_WARM (1 = run warm pass too)

import { spawn } from "node:child_process";
import { setTimeout as sleep } from "node:timers/promises";
import { chromium } from "playwright";

const PORT = Number(process.env.E2E_PORT ?? 4189);
const BASE = `http://127.0.0.1:${PORT}`;
const CPU_RATE = Number(process.env.PERF_CPU_RATE ?? 4);
const WARM = process.env.PERF_WARM !== "0";

const INIT_SCRIPT = `
  window.__lt = [];
  window.__fr = [];
  new PerformanceObserver((list) => {
    for (const e of list.getEntries()) window.__lt.push({ s: e.startTime, d: e.duration });
  }).observe({ entryTypes: ["longtask"] });
  let __last = performance.now();
  const __tick = (t) => {
    window.__fr.push({ t, g: t - __last });
    __last = t;
    requestAnimationFrame(__tick);
  };
  requestAnimationFrame(__tick);
`;

const resetBuffers = (page) =>
  page.evaluate(() => {
    window.__lt.length = 0;
    window.__fr.length = 0;
  });

const snapshot = (page) =>
  page.evaluate(() => {
    const tasks = window.__lt.map((e) => ({ s: Math.round(e.s), d: Math.round(e.d) }));
    const gaps = window.__fr.slice(1).map((e) => ({ t: Math.round(e.t), g: e.g }));
    const durations = gaps.map((f) => f.g).sort((a, b) => a - b);
    const pct = (p) =>
      durations.length ? Math.round(durations[Math.min(durations.length - 1, Math.floor(durations.length * p))]) : 0;
    return {
      longTasks: tasks.length,
      ltTotal: tasks.reduce((n, e) => n + e.d, 0),
      ltMax: tasks.reduce((m, e) => Math.max(m, e.d), 0),
      ltList: tasks,
      frames: gaps.length,
      over33: gaps.filter((f) => f.g > 33.4).length,
      over50: gaps.filter((f) => f.g > 50).length,
      maxGap: gaps.reduce((m, f) => Math.max(m, f.g), 0),
      p95: pct(0.95),
    };
  });

function fmt(row) {
  const pad = (v, n, r = true) => String(v).padStart(r ? n : n, " ");
  return [
    pad(row.name, 11, false),
    pad(row.longTasks, 4),
    pad(row.ltTotal, 7),
    pad(row.ltMax, 6),
    pad(row.frames, 7),
    pad(row.over33, 6),
    pad(row.over50, 6),
    pad(row.maxGap, 7),
    pad(row.p95, 5),
  ].join("  ");
}

async function waitForServer() {
  for (let i = 0; i < 100; i++) {
    try {
      const res = await fetch(`${BASE}/healthz`);
      if (res.ok) return;
    } catch {}
    await sleep(100);
  }
  throw new Error(`mock server did not come up on ${BASE}`);
}

async function runScenarios(browser) {
  const rows = [];

  // Desktop: cold home load (route transition + first paint, then idle work).
  const desktop = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await desktop.newPage();
  await page.addInitScript(INIT_SCRIPT);
  const cdp = await desktop.newCDPSession(page);
  await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU_RATE });

  await page.goto(`${BASE}/`, { waitUntil: "commit" });
  await page.waitForSelector(".gallery-card", { timeout: 15_000 });
  const early = await snapshot(page);
  rows.push({ name: "open-early", ...early });

  await resetBuffers(page);
  await sleep(5_000);
  const idle = await snapshot(page);
  rows.push({ name: "open-idle", ...idle });

  // Scroll: wheel-scroll the (infinite) home list; pages keep arriving.
  await resetBuffers(page);
  for (let i = 0; i < 45; i++) {
    await page.mouse.wheel(0, 600);
    await sleep(70);
  }
  await sleep(300);
  const scroll = await snapshot(page);
  rows.push({ name: "scroll", ...scroll });

  // Route: SPA navigation so PageTransition runs, back to home again.
  await resetBuffers(page);
  await page.click('a[href="/popular"]');
  await page.waitForSelector(".gallery-card", { timeout: 15_000 });
  await sleep(400);
  await page.click('a[href="/"]');
  await page.waitForSelector(".gallery-card", { timeout: 15_000 });
  await sleep(400);
  const route = await snapshot(page);
  rows.push({ name: "route", ...route });

  await page.close();
  await desktop.close();

  // Mobile: header drawer open/close.
  const mobile = await browser.newContext({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
  });
  const mpage = await mobile.newPage();
  await mpage.addInitScript(INIT_SCRIPT);
  const mcdp = await mobile.newCDPSession(mpage);
  await mcdp.send("Emulation.setCPUThrottlingRate", { rate: CPU_RATE });

  await mpage.goto(`${BASE}/`, { waitUntil: "networkidle" });
  await mpage.waitForSelector(".gallery-card", { timeout: 15_000 });
  await sleep(600);
  await resetBuffers(mpage);
  await mpage.tap('button[aria-label="打开菜单"]');
  await sleep(450);
  await mpage.tap('button[aria-label="关闭菜单"]');
  await sleep(450);
  const drawer = await snapshot(mpage);
  rows.push({ name: "drawer", ...drawer });

  await mpage.close();
  await mobile.close();

  return rows;
}

function print(title, rows) {
  console.log(`\n== ${title} (CPU ${CPU_RATE}x) ==`);
  console.log(
    ["scenario", "tasks", "ltMs", "ltMax", "frames", ">33ms", ">50ms", "maxGap", "p95"].map((h, i) =>
      h.padStart(i === 0 ? 11 : i === 1 ? 4 : 7),
    ).join("  "),
  );
  for (const row of rows) console.log(fmt(row));
}

const server = spawn("bun", ["e2e/mock-server.mjs"], {
  env: { ...process.env, E2E_PORT: String(PORT) },
  stdio: ["ignore", "inherit", "inherit"],
});

let browser;
try {
  await waitForServer();
  browser = await chromium.launch({ headless: true });

  const first = await runScenarios(browser);
  print("cold run", first);
  if (WARM) {
    const warm = await runScenarios(browser);
    print("warm run (module/tag caches hot)", warm);
  }
} finally {
  await browser?.close();
  server.kill();
}
