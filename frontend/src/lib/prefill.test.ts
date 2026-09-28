import { describe, expect, it } from "vitest";
import {
  hasActivePrefillJob,
  isActivePrefillStatus,
  isTerminalPrefillStatus,
  prefillProgressPercent,
  prefillProgressText,
  prefillStatusLabel,
} from "./prefill";
import type { PrefillJob } from "../types/prefill";

function makeJob(overrides: Partial<PrefillJob> = {}): PrefillJob {
  return {
    id: 1,
    gallery_id: null,
    gallery_token: "",
    title: "test",
    status: "completed",
    total: 10,
    progress: null,
    failed_count: 0,
    errors: [],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    finished_at: "2026-01-01T00:10:00Z",
    ...overrides,
  };
}

describe("prefill status helpers", () => {
  it("classifies active and terminal statuses", () => {
    expect(isActivePrefillStatus("queued")).toBe(true);
    expect(isActivePrefillStatus("running")).toBe(true);
    expect(isActivePrefillStatus("completed")).toBe(false);
    expect(isTerminalPrefillStatus("completed")).toBe(true);
    expect(isTerminalPrefillStatus("cancelled")).toBe(true);
    expect(isTerminalPrefillStatus("failed")).toBe(true);
    expect(isTerminalPrefillStatus("running")).toBe(false);
  });

  it("detects jobs that are still active in a list", () => {
    expect(hasActivePrefillJob([])).toBe(false);
    expect(hasActivePrefillJob([makeJob({ status: "completed" })])).toBe(false);
    expect(hasActivePrefillJob([makeJob({ status: "queued" })])).toBe(true);
    expect(
      hasActivePrefillJob([
        makeJob({ id: 1, status: "completed" }),
        makeJob({ id: 2, status: "running" }),
      ]),
    ).toBe(true);
  });

  it("maps statuses to Chinese labels", () => {
    expect(prefillStatusLabel("queued")).toBe("排队中");
    expect(prefillStatusLabel("running")).toBe("下载中");
    expect(prefillStatusLabel("completed")).toBe("已完成");
    expect(prefillStatusLabel("cancelled")).toBe("已取消");
    expect(prefillStatusLabel("failed")).toBe("失败");
  });
});

describe("prefill progress helpers", () => {
  it("computes percent from in-memory progress", () => {
    const job = makeJob({
      status: "running",
      total: 8,
      progress: { done: 3, cached: 1, fetched: 2 },
    });
    expect(prefillProgressPercent(job)).toBe(38);
  });

  it("returns 100 for completed jobs and 0 without progress", () => {
    expect(prefillProgressPercent(makeJob({ status: "completed" }))).toBe(100);
    expect(prefillProgressPercent(makeJob({ status: "queued" }))).toBe(0);
    expect(prefillProgressPercent(makeJob({ total: 0, status: "running" }))).toBe(0);
  });

  it("clamps percent at 100", () => {
    const job = makeJob({
      status: "running",
      total: 4,
      progress: { done: 9, cached: 0, fetched: 9 },
    });
    expect(prefillProgressPercent(job)).toBe(100);
  });

  it("formats progress text with done/total and counters", () => {
    const job = makeJob({
      status: "running",
      total: 5,
      progress: { done: 2, cached: 1, fetched: 1 },
    });
    expect(prefillProgressText(job)).toBe("2 / 5 页 · 缓存 1 · 抓取 1");
  });

  it("falls back to total when there is no progress", () => {
    expect(prefillProgressText(makeJob({ status: "queued", total: 12 }))).toBe(
      "12 页",
    );
  });
});
