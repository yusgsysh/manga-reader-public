import { beforeEach, describe, expect, it } from "vitest";
import { tagTranslationService } from "./service";

const fixture = {
  version: 7,
  data: [
    {
      namespace: "rows",
      count: 13,
      data: {
        female: { name: "<p>女性</p>" },
      },
    },
    {
      namespace: "female",
      count: 612,
      data: {
        yuri: { name: "<p>百合</p>" },
      },
    },
    {
      namespace: "artist",
      count: 15614,
      data: {
        "foo bar": { name: "<p>富棒</p>" },
      },
    },
  ],
};

describe("tagTranslationService", () => {
  beforeEach(() => {
    tagTranslationService.loadFromData(fixture);
  });

  it("translates a known tag", () => {
    expect(
      tagTranslationService.translateTag({ namespace: "female", name: "yuri" }),
    ).toBe("百合");
  });

  it("falls back to namespace:name for unknown tags", () => {
    expect(
      tagTranslationService.translateTag({
        namespace: "female",
        name: "unknown_tag",
      }),
    ).toBe("female:unknown_tag");
  });

  it("falls back to name for empty namespace", () => {
    expect(
      tagTranslationService.translateTag({
        namespace: "",
        name: "something",
      }),
    ).toBe("something");
  });

  it("matches case-insensitively against lowercase db keys", () => {
    expect(
      tagTranslationService.translateTag({
        namespace: "artist",
        name: "Foo Bar",
      }),
    ).toBe("富棒");
    expect(
      tagTranslationService.translateTag({
        namespace: "female",
        name: "YURI",
      }),
    ).toBe("百合");
  });

  it("reports whether a tag has a translation", () => {
    expect(
      tagTranslationService.hasTranslation({
        namespace: "female",
        name: "yuri",
      }),
    ).toBe(true);
    expect(
      tagTranslationService.hasTranslation({
        namespace: "female",
        name: "unknown_tag",
      }),
    ).toBe(false);
  });

  it("translates namespaces from rows data", () => {
    expect(tagTranslationService.translateNamespace("female")).toBe("女性");
    expect(tagTranslationService.translateNamespace("unknown")).toBe(
      "unknown",
    );
  });
});
