import { describe, expect, it } from "vitest";
import { getTagKey, parseDb } from "./parser";

const fixture = {
  version: 7,
  data: [
    {
      namespace: "rows",
      count: 13,
      data: {
        female: { name: "<p>女性</p>" },
        male: { name: "<p>男性</p>" },
      },
    },
    {
      namespace: "female",
      count: 612,
      data: {
        yuri: { name: "<p>百合</p>", intro: "<p>百合题材</p>" },
      },
    },
    {
      namespace: "artist",
      count: 15614,
      data: {
        "Foo Bar": { name: "<p>富棒</p>" },
      },
    },
  ],
};

describe("parseDb", () => {
  const index = parseDb(fixture);

  it("builds a translation map", () => {
    const entry = index.translationMap.get("female:yuri");
    expect(entry?.translation).toBe("百合");
    expect(entry?.description).toBe("百合题材");
  });

  it("strips html tags from names", () => {
    expect(index.translationMap.get("female:yuri")?.translation).toBe("百合");
    expect(index.translationMap.get("artist:Foo Bar")?.translation).toBe(
      "富棒",
    );
  });

  it("builds the namespace map from rows data", () => {
    expect(index.namespaceMap.get("female")).toBe("女性");
    expect(index.namespaceMap.get("male")).toBe("男性");
  });

  it("records version", () => {
    expect(index.version).toBe("7");
  });
});

describe("getTagKey", () => {
  it("returns namespace:name", () => {
    expect(getTagKey({ namespace: "female", name: "yuri" })).toBe(
      "female:yuri",
    );
  });

  it("returns :name for empty namespace", () => {
    expect(getTagKey({ namespace: "", name: "full color" })).toBe(
      ":full color",
    );
  });
});
