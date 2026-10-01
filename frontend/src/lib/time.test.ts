import { describe, expect, it } from "vitest";
import {
  formatDate,
  formatDateTime,
  formatPosted,
  formatRelativeTime,
} from "./time";

describe("formatRelativeTime", () => {
  const now = Date.now();

  it("returns empty for missing/invalid input", () => {
    expect(formatRelativeTime()).toBe("");
    expect(formatRelativeTime(null)).toBe("");
    expect(formatRelativeTime("not-a-date")).toBe("");
  });

  it("formats just now", () => {
    expect(formatRelativeTime(new Date(now - 30_000).toISOString())).toBe(
      "刚刚",
    );
  });

  it("formats minutes", () => {
    expect(formatRelativeTime(new Date(now - 10 * 60_000).toISOString())).toBe(
      "10 分钟前",
    );
  });

  it("formats hours", () => {
    expect(formatRelativeTime(new Date(now - 3 * 3_600_000).toISOString())).toBe(
      "3 小时前",
    );
  });

  it("formats yesterday", () => {
    expect(
      formatRelativeTime(new Date(now - 26 * 3_600_000).toISOString()),
    ).toBe("昨天");
  });

  it("formats days", () => {
    expect(
      formatRelativeTime(new Date(now - 5 * 86_400_000).toISOString()),
    ).toBe("5 天前");
  });
});

describe("formatPosted", () => {
  it("passes date-only values through unchanged", () => {
    expect(formatPosted("2024-01-01")).toBe("2024-01-01");
  });

  it("converts a UTC datetime to the local timezone", () => {
    const utc = new Date(Date.UTC(2026, 7, 29, 10, 0));
    const expected = formatDateTime(utc);
    expect(formatPosted("2026-08-29 10:00")).toBe(expected);
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
