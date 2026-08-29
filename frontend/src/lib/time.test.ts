import { describe, expect, it } from "vitest";
import { formatRelativeTime } from "./time";

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
