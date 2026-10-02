import { describe, expect, it } from "vitest";
import {
  ALL_CATEGORY_VALUES,
  CATEGORY_META,
  categoryLabel,
  categoryPillStyle,
} from "./categories";

describe("category metadata", () => {
  it("maps every backend category value to a source color", () => {
    for (const value of ALL_CATEGORY_VALUES) {
      const meta = CATEGORY_META[value];
      expect(meta.label).toBeTruthy();
      expect(meta.from).toMatch(/^#[0-9a-f]{6}$/i);
      expect(meta.to).toMatch(/^#[0-9a-f]{6}$/i);
      expect(meta.border).toMatch(/^#[0-9a-f]{6}$/i);
    }
  });

  it("uses the source poster colors", () => {
    expect(categoryPillStyle("doujinshi").backgroundImage).toBe(
      "radial-gradient(#fc4e4e, #f26f5f)",
    );
    expect(categoryPillStyle("doujinshi").borderColor).toBe("#fc4e4e");
    expect(categoryPillStyle("image-set").backgroundImage).toBe(
      "radial-gradient(#2756aa, #5f5fff)",
    );
    expect(categoryPillStyle("asianporn").borderColor).toBe("#b452a5");
  });

  it("resolves labels and falls back to Other", () => {
    expect(categoryLabel("artistcg")).toBe("Artist CG");
    expect(categoryLabel("non-h")).toBe("Non-H");
    expect(categoryLabel("image-set")).toBe("Image Set");
    expect(categoryLabel("unknown-cat")).toBe("Other");
    expect(categoryLabel(undefined)).toBe("Other");
  });

  it("lists all source categories in source order", () => {
    expect(ALL_CATEGORY_VALUES).toEqual([
      "doujinshi",
      "manga",
      "artistcg",
      "gamecg",
      "western",
      "non-h",
      "image-set",
      "cosplay",
      "asianporn",
      "misc",
    ]);
  });
});
