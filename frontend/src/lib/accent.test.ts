import { describe, expect, it } from "vitest";
import {
  ACCENT_PRESETS,
  accentContrast,
  isPresetAccent,
  isValidHexColor,
  normalizeAccent,
} from "./accent";

describe("isValidHexColor", () => {
  it("accepts 3- and 6-digit hex with or without #", () => {
    expect(isValidHexColor("#2563eb")).toBe(true);
    expect(isValidHexColor("2563eb")).toBe(true);
    expect(isValidHexColor("#abc")).toBe(true);
  });

  it("rejects other formats", () => {
    expect(isValidHexColor("")).toBe(false);
    expect(isValidHexColor("#12345")).toBe(false);
    expect(isValidHexColor("rgb(1,2,3)")).toBe(false);
    expect(isValidHexColor("red")).toBe(false);
  });
});

describe("normalizeAccent", () => {
  it("expands shorthand and lowercases", () => {
    expect(normalizeAccent("#ABC")).toBe("#aabbcc");
    expect(normalizeAccent("2563EB")).toBe("#2563eb");
  });
});

describe("accentContrast", () => {
  it("uses white text on dark accents and dark text on light accents", () => {
    expect(accentContrast("#2563eb")).toBe("#ffffff");
    expect(accentContrast("#fde047")).toBe("#0b1220");
  });
});

describe("isPresetAccent", () => {
  it("matches presets case-insensitively and rejects null/custom", () => {
    expect(isPresetAccent(ACCENT_PRESETS[0].color)).toBe(true);
    expect(isPresetAccent(ACCENT_PRESETS[0].color.toUpperCase())).toBe(true);
    expect(isPresetAccent("#123456")).toBe(false);
    expect(isPresetAccent(null)).toBe(false);
  });
});
