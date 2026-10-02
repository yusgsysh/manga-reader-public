import type { AdvancedSearchOptions } from "../../types/gallery";
import { ALL_CATEGORY_VALUES, CATEGORY_META } from "../../lib/categories";

export const CATEGORIES = ALL_CATEGORY_VALUES.map((value) => ({
  value,
  label: CATEGORY_META[value].label,
}));

export const MIN_RATING_OPTIONS = [
  { value: "", label: "Any rating" },
  { value: "2", label: "2+" },
  { value: "3", label: "3+" },
  { value: "4", label: "4+" },
  { value: "5", label: "5" },
];

export const ADVANCED_LABELS: Record<string, string> = {
  min_pages: "最小页数",
  max_pages: "最大页数",
  min_rating: "最低评分",
  has_torrent: "有种子",
  include_expunged: "含已删除",
  disable_language_filter: "不限语言",
  disable_uploader_filter: "不限上传者",
  disable_tag_filter: "不过滤标签",
};

const ADVANCED_KEYS = Object.keys(ADVANCED_LABELS) as Array<
  keyof AdvancedSearchOptions
>;

// ExHentai treats an empty or zero page bound as "unset" (search_presubmit
// disables the field before submit), so `0` must never reach the query string.
export function normalizePageBound(value: unknown): number | undefined {
  const n = typeof value === "number" ? value : Number(value);
  return Number.isFinite(n) && n > 0 ? n : undefined;
}

export function parseAdvancedParams(
  searchParams: URLSearchParams,
): AdvancedSearchOptions {
  const minPages = normalizePageBound(searchParams.get("min_pages"));
  const maxPages = normalizePageBound(searchParams.get("max_pages"));
  const minRating = normalizePageBound(searchParams.get("min_rating"));
  return {
    min_pages: minPages,
    max_pages: maxPages,
    min_rating: minRating,
    has_torrent: searchParams.get("has_torrent") === "true",
    include_expunged: searchParams.get("include_expunged") === "true",
    disable_language_filter: searchParams.get("disable_language_filter") === "true",
    disable_uploader_filter: searchParams.get("disable_uploader_filter") === "true",
    disable_tag_filter: searchParams.get("disable_tag_filter") === "true",
  };
}

export function isActiveValue(value: unknown): boolean {
  return value !== undefined && value !== false && value !== "" && value !== 0;
}

// Serializes the advanced options into URL params, dropping inactive values and
// zero page bounds so the resulting request matches the ExHentai form.
export function serializeAdvancedParams(
  options: AdvancedSearchOptions,
): URLSearchParams {
  const params = new URLSearchParams();
  for (const key of ADVANCED_KEYS) {
    const value = options[key];
    if (!isActiveValue(value)) continue;
    params.set(key, String(value));
  }
  return params;
}

export function countActiveFilters(options: AdvancedSearchOptions): number {
  let count = 0;
  for (const key of ADVANCED_KEYS) {
    if (isActiveValue(options[key])) count++;
  }
  return count;
}

export function pickAdvancedOptions(
  filters: AdvancedSearchOptions,
): AdvancedSearchOptions {
  const picked: AdvancedSearchOptions = {};
  for (const key of ADVANCED_KEYS) {
    const value = filters[key];
    if (isActiveValue(value)) {
      (picked as Record<string, unknown>)[key] = value;
    }
  }
  return picked;
}
