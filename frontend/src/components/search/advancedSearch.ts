import type { AdvancedSearchOptions } from "../../types/gallery";

export const CATEGORIES = [
  { value: "doujinshi", label: "Doujinshi" },
  { value: "manga", label: "Manga" },
  { value: "artistcg", label: "Artist CG" },
  { value: "gamecg", label: "Game CG" },
  { value: "western", label: "Western" },
  { value: "cosplay", label: "Cosplay" },
  { value: "asianporn", label: "Asian Porn" },
  { value: "non-h", label: "Non-H" },
  { value: "misc", label: "Misc" },
];

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
  search_name: "搜索标题",
  search_tags: "搜索标签",
  search_description: "搜索描述",
  include_low_power_tags: "含低权重标签",
  include_downvoted_tags: "含被降权标签",
  disable_language_filter: "不限语言",
  disable_uploader_filter: "不限上传者",
  disable_tag_filter: "不过滤标签",
};

export const ADVANCED_KEYS = Object.keys(ADVANCED_LABELS) as Array<
  keyof AdvancedSearchOptions
>;

export function parseAdvancedParams(
  searchParams: URLSearchParams,
): AdvancedSearchOptions {
  return {
    min_pages: searchParams.get("min_pages")
      ? Number(searchParams.get("min_pages"))
      : undefined,
    max_pages: searchParams.get("max_pages")
      ? Number(searchParams.get("max_pages"))
      : undefined,
    min_rating: searchParams.get("min_rating")
      ? Number(searchParams.get("min_rating"))
      : undefined,
    has_torrent: searchParams.get("has_torrent") === "true",
    include_expunged: searchParams.get("include_expunged") === "true",
    search_name: searchParams.get("search_name") === "true",
    search_tags: searchParams.get("search_tags") === "true",
    search_description: searchParams.get("search_description") === "true",
    include_low_power_tags: searchParams.get("include_low_power_tags") === "true",
    include_downvoted_tags: searchParams.get("include_downvoted_tags") === "true",
    disable_language_filter: searchParams.get("disable_language_filter") === "true",
    disable_uploader_filter: searchParams.get("disable_uploader_filter") === "true",
    disable_tag_filter: searchParams.get("disable_tag_filter") === "true",
  };
}

export function isActiveValue(value: unknown): boolean {
  return value !== undefined && value !== false && value !== "";
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
