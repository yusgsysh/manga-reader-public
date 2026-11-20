import { describe, expect, it } from "vitest";
import {
  countActiveFilters,
  isActiveValue,
  normalizePageBound,
  parseAdvancedParams,
  serializeAdvancedParams,
} from "./advancedSearch";

describe("normalizePageBound", () => {
  it("treats empty, zero and invalid values as unset", () => {
    expect(normalizePageBound("")).toBeUndefined();
    expect(normalizePageBound("0")).toBeUndefined();
    expect(normalizePageBound(0)).toBeUndefined();
    expect(normalizePageBound("-3")).toBeUndefined();
    expect(normalizePageBound("abc")).toBeUndefined();
  });

  it("keeps positive values", () => {
    expect(normalizePageBound("10")).toBe(10);
    expect(normalizePageBound(200)).toBe(200);
  });
});

describe("parseAdvancedParams", () => {
  it("drops zero page bounds", () => {
    const params = new URLSearchParams("min_pages=0&max_pages=0&min_rating=4");
    const parsed = parseAdvancedParams(params);
    expect(parsed.min_pages).toBeUndefined();
    expect(parsed.max_pages).toBeUndefined();
    expect(parsed.min_rating).toBe(4);
  });

  it("ignores removed legacy search-target params", () => {
    const params = new URLSearchParams("search_name=true&search_tags=true");
    const parsed = parseAdvancedParams(params);
    expect(parsed).not.toHaveProperty("search_name");
    expect(parsed).not.toHaveProperty("search_tags");
  });

  it("reads boolean flags", () => {
    const params = new URLSearchParams(
      "has_torrent=true&include_expunged=true&disable_tag_filter=true",
    );
    const parsed = parseAdvancedParams(params);
    expect(parsed.has_torrent).toBe(true);
    expect(parsed.include_expunged).toBe(true);
    expect(parsed.disable_tag_filter).toBe(true);
  });
});

describe("serializeAdvancedParams", () => {
  it("omits inactive values and zero page bounds", () => {
    const out = serializeAdvancedParams({
      min_pages: 0,
      max_pages: 200,
      min_rating: 4,
      has_torrent: false,
      include_expunged: false,
    });
    expect(out.toString()).toBe("max_pages=200&min_rating=4");
  });

  it("keeps active boolean flags", () => {
    const out = serializeAdvancedParams({ has_torrent: true });
    expect(out.get("has_torrent")).toBe("true");
    expect(out.get("include_expunged")).toBeNull();
  });
});

describe("isActiveValue / countActiveFilters", () => {
  it("does not count zero or false as active", () => {
    expect(isActiveValue(0)).toBe(false);
    expect(isActiveValue(false)).toBe(false);
    expect(isActiveValue("")).toBe(false);
    expect(countActiveFilters({ min_pages: 0, max_pages: 5 })).toBe(1);
  });
});
