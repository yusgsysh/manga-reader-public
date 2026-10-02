import type { CSSProperties } from "react";
import type { GalleryCategory } from "../types/gallery";

// Category colors are copied from the ExHentai/E-Hentai stylesheet
// (https://e-hentai.org/z/0381/g.css): `.ct1`–`.cta`. The two sites share the
// same frontend, so these match the source category pills exactly.
export interface CategoryMeta {
  label: string;
  from: string;
  to: string;
  border: string;
}

export const CATEGORY_META: Record<GalleryCategory, CategoryMeta> = {
  doujinshi: { label: "Doujinshi", from: "#fc4e4e", to: "#f26f5f", border: "#fc4e4e" },
  manga: { label: "Manga", from: "#e78c1a", to: "#fcb417", border: "#e78c1a" },
  artistcg: { label: "Artist CG", from: "#c7bf07", to: "#dde500", border: "#c7bf07" },
  gamecg: { label: "Game CG", from: "#1a9317", to: "#05bf0b", border: "#1a9317" },
  western: { label: "Western", from: "#5dc13b", to: "#14e723", border: "#5dc13b" },
  "non-h": { label: "Non-H", from: "#0f9ebd", to: "#08d7e2", border: "#0f9ebd" },
  "image-set": { label: "Image Set", from: "#2756aa", to: "#5f5fff", border: "#2756aa" },
  cosplay: { label: "Cosplay", from: "#8800c3", to: "#9755f5", border: "#8800c3" },
  asianporn: { label: "Asian Porn", from: "#b452a5", to: "#fe93ff", border: "#b452a5" },
  misc: { label: "Misc", from: "#707070", to: "#9e9e9e", border: "#707070" },
  other: { label: "Other", from: "#707070", to: "#9e9e9e", border: "#707070" },
};

// Source order: row one is doujinshi → western, row two is non-h → misc.
export const ALL_CATEGORY_VALUES: GalleryCategory[] = [
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
];

export function categoryMeta(category?: string): CategoryMeta {
  if (category && category in CATEGORY_META) {
    return CATEGORY_META[category as GalleryCategory];
  }
  return CATEGORY_META.other;
}

export function categoryLabel(category?: string): string {
  return categoryMeta(category).label;
}

// Returns the included categories in source order. An empty/undefined list
// means "all included" (ExHentai includes every category by default).
export function normalizeIncludedCategories(
  categories?: string[],
): GalleryCategory[] {
  if (!categories || categories.length === 0) return [...ALL_CATEGORY_VALUES];
  const set = new Set(categories);
  return ALL_CATEGORY_VALUES.filter((value) => set.has(value));
}

// Toggles one category's inclusion, keeping source order and never allowing
// the last included category to be removed.
export function toggleIncludedCategory(
  included: string[],
  category: string,
): string[] {
  const next = included.includes(category)
    ? included.filter((value) => value !== category)
    : [...included, category];
  if (next.length === 0) return included;
  return ALL_CATEGORY_VALUES.filter((value) => next.includes(value));
}

// Mirrors the source category color. The app keeps its own pill shape and
// typography; only the fill color is taken from the source `.ct*` classes.
export function categoryPillStyle(category?: string): CSSProperties {
  const meta = categoryMeta(category);
  return {
    backgroundColor: meta.from,
    color: "#ffffff",
  };
}
