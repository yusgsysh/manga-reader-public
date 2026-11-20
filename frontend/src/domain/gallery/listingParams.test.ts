import { describe, expect, it } from "vitest";
import {
  parseListFilters,
  parseNavOptions,
  serializeListFilters,
  serializeNavOptions,
  withListFilters,
  withNavOptions,
} from "./listingParams";

describe("parseListFilters", () => {
  it("returns an empty filter set for an empty URL", () => {
    expect(parseListFilters(new URLSearchParams())).toEqual({});
  });

  it("drops inactive advanced options", () => {
    const params = new URLSearchParams(
      "min_pages=0&max_pages=0&has_torrent=false&min_rating=4",
    );
    expect(parseListFilters(params)).toEqual({ min_rating: 4 });
  });

  it("reads tags and categories as lists", () => {
    const params = new URLSearchParams("tags=a,b&categories=doujinshi,manga");
    const filters = parseListFilters(params);
    expect(filters.tags).toEqual(["a", "b"]);
    expect(filters.categories).toEqual(["doujinshi", "manga"]);
  });
});

describe("serializeListFilters", () => {
  it("serializes tags, categories and active advanced options", () => {
    const out = serializeListFilters({
      tags: ["a", "b"],
      categories: ["doujinshi"],
      min_rating: 4,
      has_torrent: true,
      max_pages: 0,
    });
    expect(out.get("tags")).toBe("a,b");
    expect(out.get("categories")).toBe("doujinshi");
    expect(out.get("min_rating")).toBe("4");
    expect(out.get("has_torrent")).toBe("true");
    expect(out.get("max_pages")).toBeNull();
  });

  it("round-trips through parseListFilters", () => {
    const filters = {
      tags: ["a", "b"],
      categories: ["doujinshi", "manga"],
      min_pages: 10,
      include_expunged: true,
    };
    expect(parseListFilters(serializeListFilters(filters))).toEqual(filters);
  });
});

describe("parseNavOptions / serializeNavOptions", () => {
  it("parses seek and jump", () => {
    const nav = parseNavOptions(new URLSearchParams("seek=2020-01&jump=1w"));
    expect(nav).toEqual({ seek: "2020-01", jump: "1w" });
  });

  it("returns undefined fields when absent", () => {
    expect(parseNavOptions(new URLSearchParams())).toEqual({
      seek: undefined,
      jump: undefined,
    });
  });

  it("serializes only set options", () => {
    expect(serializeNavOptions({ seek: "2020-01" }).toString()).toBe(
      "seek=2020-01",
    );
    expect(serializeNavOptions({}).toString()).toBe("");
  });
});

describe("withListFilters / withNavOptions", () => {
  it("replaces only the relevant params", () => {
    const base = new URLSearchParams("seek=1w&min_rating=4&unrelated=keep");
    const next = withListFilters(base, { tags: ["a"] });
    expect(next.get("tags")).toBe("a");
    expect(next.get("min_rating")).toBeNull();
    expect(next.get("seek")).toBe("1w");
    expect(next.get("unrelated")).toBe("keep");
  });

  it("clears nav options when given an empty object", () => {
    const base = new URLSearchParams("seek=1w&jump=3d");
    expect(withNavOptions(base, {}).toString()).toBe("");
  });
});
