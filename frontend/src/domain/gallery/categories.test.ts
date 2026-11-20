import { describe, expect, it } from "vitest";
import {
  ALL_CATEGORY_VALUES,
  CATEGORY_META,
  categoryLabel,
  categoryPillStyle,
  normalizeIncludedCategories,
  toggleIncludedCategory,
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

  it("uses the source category colors as a solid fill", () => {
    expect(categoryPillStyle("doujinshi").backgroundColor).toBe("#fc4e4e");
    expect(categoryPillStyle("manga").backgroundColor).toBe("#e78c1a");
    expect(categoryPillStyle("image-set").backgroundColor).toBe("#2756aa");
    expect(categoryPillStyle("asianporn").backgroundColor).toBe("#b452a5");
    expect(categoryPillStyle("doujinshi").color).toBe("#ffffff");
  });

  it("resolves labels and falls back to Other", () => {
    expect(categoryLabel("artistcg")).toBe("Artist CG");
    expect(categoryLabel("non-h")).toBe("Non-H");
    expect(categoryLabel("image-set")).toBe("Image Set");
    expect(categoryLabel("unknown-cat")).toBe("Other");
    expect(categoryLabel(undefined)).toBe("Other");
  });

  it("normalizes included categories to source order, defaulting to all", () => {
    expect(normalizeIncludedCategories(undefined)).toEqual(ALL_CATEGORY_VALUES);
    expect(normalizeIncludedCategories([])).toEqual(ALL_CATEGORY_VALUES);
    expect(normalizeIncludedCategories(["misc", "doujinshi"])).toEqual([
      "doujinshi",
      "misc",
    ]);
  });

  it("toggles inclusion and never removes the last category", () => {
    expect(toggleIncludedCategory([...ALL_CATEGORY_VALUES], "doujinshi")).toEqual(
      ALL_CATEGORY_VALUES.filter((value) => value !== "doujinshi"),
    );
    expect(toggleIncludedCategory(["misc"], "doujinshi")).toEqual([
      "doujinshi",
      "misc",
    ]);
    expect(toggleIncludedCategory(["misc"], "misc")).toEqual(["misc"]);
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
