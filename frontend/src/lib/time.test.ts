import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  formatDate,
  formatDateTime,
  formatPosted,
  formatRelativeTime,
} from "./time";

describe("formatRelativeTime", () => {
  // Pin the clock: these assertions are about the elapsed-time thresholds, not
  // about whatever time the test happens to run at.
  const now = new Date("2026-08-29T10:00:00Z");
  const ago = (ms: number) =>
    new Date(now.getTime() - ms).toISOString();

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(now);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns empty for missing/invalid input", () => {
    expect(formatRelativeTime()).toBe("");
    expect(formatRelativeTime(null)).toBe("");
    expect(formatRelativeTime("not-a-date")).toBe("");
  });

  it("formats just now, minutes, hours, yesterday and days", () => {
    expect(formatRelativeTime(ago(30_000))).toBe("刚刚");
    expect(formatRelativeTime(ago(10 * 60_000))).toBe("10分钟前");
    expect(formatRelativeTime(ago(3 * 3_600_000))).toBe("3小时前");
    expect(formatRelativeTime(ago(26 * 3_600_000))).toBe("昨天");
    expect(formatRelativeTime(ago(5 * 86_400_000))).toBe("5天前");
  });
});

describe("formatPosted", () => {
  const pad = (n: number) => String(n).padStart(2, "0");
  const utc = new Date(Date.UTC(2026, 7, 29, 10, 0));

  it("renders date-only values as Y/M/D", () => {
    expect(formatPosted("2024-01-01")).toBe("2024/1/1");
  });

  it("formats recent datetimes in minutes", () => {
    const now = new Date(utc.getTime() + 10 * 60_000);
    expect(formatPosted("2026-08-29 10:00", now)).toBe("10分钟前");
  });

  it("formats earlier-today datetimes with a 今天 prefix", () => {
    const local = new Date(utc.getTime());
    const now = new Date(local);
    now.setHours(local.getHours() + 2);
    expect(formatPosted("2026-08-29 10:00", now)).toBe(
      `今天${pad(local.getHours())}:${pad(local.getMinutes())}`,
    );
  });

  it("formats earlier-this-year datetimes as M/D HH:mm", () => {
    const local = new Date(utc.getTime());
    const now = new Date(Date.UTC(2026, 8, 15, 10, 0));
    expect(formatPosted("2026-08-29 10:00", now)).toBe(
      `${local.getMonth() + 1}/${local.getDate()} ${pad(local.getHours())}:${pad(local.getMinutes())}`,
    );
  });

  it("formats datetimes from another year with the full date", () => {
    const local = new Date(utc.getTime());
    const now = new Date(Date.UTC(2027, 0, 15, 10, 0));
    expect(formatPosted("2026-08-29 10:00", now)).toBe(
      `${local.getFullYear()}/${local.getMonth() + 1}/${local.getDate()} ${pad(local.getHours())}:${pad(local.getMinutes())}`,
    );
  });

  it("handles empty/invalid input", () => {
    expect(formatPosted()).toBe("");
    expect(formatPosted(null)).toBe("");
    expect(formatPosted("not a date")).toBe("not a date");
  });
});

describe("local date formatting", () => {
  it("returns empty for invalid input", () => {
    expect(formatDate("nope")).toBe("");
    expect(formatDateTime(null)).toBe("");
  });
});
