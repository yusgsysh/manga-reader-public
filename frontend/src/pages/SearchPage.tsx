import { useSearchParams } from "react-router";
import { useSearch } from "../hooks/useSearch";
import { useTagTranslation } from "../hooks/useTagTranslation";
import { GalleryGrid } from "../components/gallery/GalleryGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";
import { Input, Button, Checkbox, Select } from "@cloudflare/kumo";
import { Search, ChevronDown, ChevronUp, X } from "lucide-react";
import { useState, useCallback, useRef, useEffect } from "react";
import { SimplePagination } from "../components/common/SimplePagination";
import type { AdvancedSearchOptions } from "../types/gallery";

const CATEGORIES = [
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

const MIN_RATING_OPTIONS = [
  { value: "", label: "Any rating" },
  { value: "2", label: "2+" },
  { value: "3", label: "3+" },
  { value: "4", label: "4+" },
  { value: "5", label: "5" },
];

function parseAdvancedParams(searchParams: URLSearchParams): AdvancedSearchOptions {
  return {
    min_pages: searchParams.get("min_pages") ? Number(searchParams.get("min_pages")) : undefined,
    max_pages: searchParams.get("max_pages") ? Number(searchParams.get("max_pages")) : undefined,
    min_rating: searchParams.get("min_rating") ? Number(searchParams.get("min_rating")) : undefined,
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

function parseTags(searchParams: URLSearchParams): string[] {
  const tags = searchParams.get("tags");
  if (!tags) return [];
  return tags.split(",").filter(Boolean);
}

function buildTagQuery(tags: string[]): string {
  return tags.map((tag) => `tag:${tag.trim()}`).join(" ");
}

function countActiveFilters(options: AdvancedSearchOptions): number {
  let count = 0;
  if (options.min_pages !== undefined) count++;
  if (options.max_pages !== undefined) count++;
  if (options.min_rating !== undefined) count++;
  if (options.has_torrent) count++;
  if (options.include_expunged) count++;
  if (options.search_name) count++;
  if (options.search_tags) count++;
  if (options.search_description) count++;
  if (options.include_low_power_tags) count++;
  if (options.include_downvoted_tags) count++;
  if (options.disable_language_filter) count++;
  if (options.disable_uploader_filter) count++;
  if (options.disable_tag_filter) count++;
  return count;
}

export function SearchPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const q = searchParams.get("q") ?? "";
  const site = searchParams.get("site") ?? "exhentai";
  const categories = searchParams.get("categories") ?? "";
  const page = Number(searchParams.get("page") ?? "0");
  const advancedOptions = parseAdvancedParams(searchParams);
  const tags = parseTags(searchParams);

  const [inputValue, setInputValue] = useState(q);
  const [tagInput, setTagInput] = useState("");
  const [pendingTags, setPendingTags] = useState<string[]>([]);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [showSuggestions, setShowSuggestions] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(-1);

  const tagInputRef = useRef<HTMLInputElement>(null);
  const suggestionsRef = useRef<HTMLDivElement>(null);

  const { searchTags, ready: tagDbReady } = useTagTranslation();
  const [suggestions, setSuggestions] = useState<Array<{ namespace: string; tag: string; translation: string }>>([]);

  const tagQuery = buildTagQuery(tags);
  const fullQuery = [q, tagQuery].filter(Boolean).join(" ");

  const { data, isLoading, error, refetch } = useSearch({
    q: fullQuery || undefined,
    site,
    categories,
    page,
    ...advancedOptions,
  });

  const updateParams = (updates: Record<string, string | null>) => {
    const params = new URLSearchParams(searchParams);
    for (const [key, value] of Object.entries(updates)) {
      if (value === null) {
        params.delete(key);
      } else {
        params.set(key, value);
      }
    }
    setSearchParams(params);
  };

  const handleSearch = () => {
    const params = new URLSearchParams();
    if (inputValue.trim()) params.set("q", inputValue.trim());
    params.set("site", site);
    if (categories) params.set("categories", categories);
    const allTags = [...tags, ...pendingTags];
    if (allTags.length > 0) params.set("tags", allTags.join(","));
    params.set("page", "0");
    setSearchParams(params);
    setPendingTags([]);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") handleSearch();
  };

  const handlePageChange = (newPage: number) => {
    updateParams({ page: String(newPage) });
  };

  const handleSiteChange = (value: string | null) => {
    updateParams({ site: value ?? "exhentai", page: "0" });
  };

  const toggleCategory = (cat: string) => {
    const current = categories ? categories.split(",") : [];
    const next = current.includes(cat)
      ? current.filter((c) => c !== cat)
      : [...current, cat];
    updateParams({ categories: next.length > 0 ? next.join(",") : null, page: "0" });
  };

  const updateAdvancedFilter = (key: keyof AdvancedSearchOptions, value: string | boolean | number | undefined) => {
    const updates: Record<string, string | null> = { page: "0" };
    if (value === undefined || value === false || value === "") {
      updates[key] = null;
    } else {
      updates[key] = String(value);
    }
    updateParams(updates);
  };

  const addPendingTag = (tagValue?: string) => {
    const tag = tagValue ?? tagInput.trim();
    if (!tag || pendingTags.includes(tag)) return;
    setPendingTags([...pendingTags, tag]);
    setTagInput("");
    setShowSuggestions(false);
    setSelectedIndex(-1);
  };

  const removePendingTag = (tagToRemove: string) => {
    setPendingTags(pendingTags.filter((t) => t !== tagToRemove));
  };

  const removeAppliedTag = (tagToRemove: string) => {
    const newTags = tags.filter((t) => t !== tagToRemove);
    updateParams({ tags: newTags.length > 0 ? newTags.join(",") : null, page: "0" });
  };

  const handleTagKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelectedIndex((prev) => Math.min(prev + 1, suggestions.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelectedIndex((prev) => Math.max(prev - 1, -1));
    } else if (e.key === "Escape") {
      setShowSuggestions(false);
      setSelectedIndex(-1);
    }
  };

  const handleTagInputChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const value = e.target.value;
      setTagInput(value);
      setSelectedIndex(-1);

      if (tagDbReady && value.trim().length > 0) {
        const results = searchTags(value.trim(), 8);
        setSuggestions(results);
        setShowSuggestions(results.length > 0);
      } else {
        setSuggestions([]);
        setShowSuggestions(false);
      }
    },
    [tagDbReady, searchTags],
  );

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (
        suggestionsRef.current &&
        !suggestionsRef.current.contains(e.target as Node) &&
        tagInputRef.current &&
        !tagInputRef.current.contains(e.target as Node)
      ) {
        setShowSuggestions(false);
      }
    };
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  const resetAdvancedFilters = () => {
    const params = new URLSearchParams();
    if (q) params.set("q", q);
    params.set("site", site);
    if (categories) params.set("categories", categories);
    params.set("page", "0");
    setSearchParams(params);
    setPendingTags([]);
    setTagInput("");
    setShowSuggestions(false);
  };

  const activeFilterCount = countActiveFilters(advancedOptions) + tags.length + pendingTags.length;

  return (
    <div className="space-y-4">
      {/* Search bar */}
      <div className="flex gap-2">
        <div className="flex-1">
          <Input
            placeholder="搜索 Gallery..."
            aria-label="搜索 Gallery"
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            onKeyDown={handleKeyDown}
          />
        </div>
        <Button onClick={handleSearch} aria-label="搜索">
          <Search className="size-4" />
        </Button>
      </div>

      {/* Filters */}
      <div className="flex flex-col gap-4">
        <div className="w-40">
          <Select
            value={site}
            onValueChange={handleSiteChange}
            aria-label="Site"
            items={[
              { label: "ExHentai", value: "exhentai" },
              { label: "E-Hentai", value: "ehentai" },
            ]}
          />
        </div>

        <div className="flex flex-wrap gap-3">
          {CATEGORIES.map((cat) => (
            <label
              key={cat.value}
              className="flex items-center gap-1.5 text-xs"
            >
              <Checkbox
                checked={categories.split(",").includes(cat.value)}
                onCheckedChange={() => toggleCategory(cat.value)}
              />
              {cat.label}
            </label>
          ))}
        </div>

        {/* Advanced Search Toggle */}
        <div>
          <button
            type="button"
            onClick={() => setShowAdvanced(!showAdvanced)}
            className="flex items-center gap-2 text-sm text-kumo-subtle hover:text-kumo-text transition-colors"
          >
            {showAdvanced ? <ChevronUp className="size-4" /> : <ChevronDown className="size-4" />}
            高级搜索
            {activeFilterCount > 0 && (
              <span className="inline-flex items-center justify-center px-2 py-0.5 text-xs font-medium rounded-full bg-kumo-accent text-white">
                {activeFilterCount}
              </span>
            )}
          </button>
        </div>

        {/* Advanced Search Panel */}
        {showAdvanced && (
          <div className="p-4 border border-kumo-border rounded-lg bg-kumo-elevated space-y-4">
            {/* Tags Input */}
            <div className="space-y-2">
              <label className="text-sm font-medium">标签</label>
              <div className="relative">
                <Input
                  ref={tagInputRef}
                  placeholder="输入标签搜索（例如：yuri, 无修正）"
                  aria-label="搜索标签"
                  aria-autocomplete="list"
                  aria-expanded={showSuggestions}
                  value={tagInput}
                  onChange={handleTagInputChange}
                  onKeyDown={handleTagKeyDown}
                  onFocus={() => {
                    if (suggestions.length > 0) setShowSuggestions(true);
                  }}
                />
                {showSuggestions && suggestions.length > 0 && (
                  <div
                    ref={suggestionsRef}
                    className="absolute z-10 top-full left-0 right-0 mt-1 bg-kumo-elevated border border-kumo-border rounded-lg shadow-lg max-h-60 overflow-y-auto"
                    role="listbox"
                  >
                    {suggestions.map((s, index) => (
                      <button
                        key={`${s.namespace}:${s.tag}`}
                        type="button"
                        role="option"
                        aria-selected={index === selectedIndex}
                        className={`w-full px-3 py-2 text-left text-sm hover:bg-kumo-subtle flex items-center justify-between ${
                          index === selectedIndex ? "bg-kumo-subtle" : ""
                        }`}
                        onClick={() => addPendingTag(`${s.namespace}:${s.tag}`)}
                        onMouseEnter={() => setSelectedIndex(index)}
                      >
                        <span className="font-mono text-xs text-kumo-subtle">
                          {s.namespace}:{s.tag}
                        </span>
                        <span className="text-sm">{s.translation}</span>
                      </button>
                    ))}
                  </div>
                )}
              </div>

              {/* Applied tags */}
              {tags.length > 0 && (
                <div className="space-y-1">
                  <p className="text-xs text-kumo-subtle">已应用的标签：</p>
                  <div className="flex flex-wrap gap-2">
                    {tags.map((tag) => (
                      <span
                        key={tag}
                        className="inline-flex items-center gap-1 px-2 py-1 text-xs bg-kumo-accent/10 text-kumo-accent rounded-md"
                      >
                        {tag}
                        <button
                          type="button"
                          onClick={() => removeAppliedTag(tag)}
                          className="hover:text-kumo-text"
                          aria-label={`移除标签 ${tag}`}
                        >
                          <X className="size-3" />
                        </button>
                      </span>
                    ))}
                  </div>
                </div>
              )}

              {/* Pending tags */}
              {pendingTags.length > 0 && (
                <div className="space-y-1">
                  <p className="text-xs text-kumo-subtle">待添加的标签（点击搜索后生效）：</p>
                  <div className="flex flex-wrap gap-2">
                    {pendingTags.map((tag) => (
                      <span
                        key={tag}
                        className="inline-flex items-center gap-1 px-2 py-1 text-xs bg-kumo-subtle rounded-md"
                      >
                        {tag}
                        <button
                          type="button"
                          onClick={() => removePendingTag(tag)}
                          className="text-kumo-subtle hover:text-kumo-text"
                          aria-label={`移除标签 ${tag}`}
                        >
                          <X className="size-3" />
                        </button>
                      </span>
                    ))}
                  </div>
                </div>
              )}

              <p className="text-xs text-kumo-subtle">
                支持 ExHentai 标签语法：普通标签（yuri）、命名空间标签（female:sole_female）
              </p>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {/* Page Range */}
              <div className="space-y-2">
                <label className="text-sm font-medium">页数范围</label>
                <div className="flex items-center gap-2">
                  <Input
                    type="number"
                    placeholder="最小页数"
                    aria-label="最小页数"
                    value={advancedOptions.min_pages ?? ""}
                    onChange={(e) => {
                      const value = e.target.value ? Number(e.target.value) : undefined;
                      updateAdvancedFilter("min_pages", value);
                    }}
                    min="0"
                    className="w-full"
                  />
                  <span className="text-kumo-subtle">-</span>
                  <Input
                    type="number"
                    placeholder="最大页数"
                    aria-label="最大页数"
                    value={advancedOptions.max_pages ?? ""}
                    onChange={(e) => {
                      const value = e.target.value ? Number(e.target.value) : undefined;
                      updateAdvancedFilter("max_pages", value);
                    }}
                    min="0"
                    className="w-full"
                  />
                </div>
              </div>

              {/* Minimum Rating */}
              <div className="space-y-2">
                <label className="text-sm font-medium">最低评分</label>
                <Select
                  value={advancedOptions.min_rating?.toString() ?? ""}
                  onValueChange={(value) => {
                    updateAdvancedFilter("min_rating", value ? Number(value) : undefined);
                  }}
                  aria-label="最低评分"
                  items={MIN_RATING_OPTIONS}
                />
              </div>
            </div>

            {/* Boolean Filters */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-2">
                <label className="text-sm font-medium">内容筛选</label>
                <div className="space-y-2">
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={advancedOptions.has_torrent ?? false}
                      onCheckedChange={(checked) => updateAdvancedFilter("has_torrent", checked)}
                    />
                    有种子
                  </label>
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={advancedOptions.include_expunged ?? false}
                      onCheckedChange={(checked) => updateAdvancedFilter("include_expunged", checked)}
                    />
                    包含已删除的 Gallery
                  </label>
                </div>
              </div>

              <div className="space-y-2">
                <label className="text-sm font-medium">搜索范围</label>
                <div className="space-y-2">
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={advancedOptions.search_tags ?? false}
                      onCheckedChange={(checked) => updateAdvancedFilter("search_tags", checked)}
                    />
                    搜索标签
                  </label>
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={advancedOptions.search_name ?? false}
                      onCheckedChange={(checked) => updateAdvancedFilter("search_name", checked)}
                    />
                    搜索标题
                  </label>
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={advancedOptions.search_description ?? false}
                      onCheckedChange={(checked) => updateAdvancedFilter("search_description", checked)}
                    />
                    搜索描述
                  </label>
                </div>
              </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-2">
                <label className="text-sm font-medium">标签选项</label>
                <div className="space-y-2">
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={advancedOptions.include_low_power_tags ?? false}
                      onCheckedChange={(checked) => updateAdvancedFilter("include_low_power_tags", checked)}
                    />
                    包含低权重标签
                  </label>
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={advancedOptions.include_downvoted_tags ?? false}
                      onCheckedChange={(checked) => updateAdvancedFilter("include_downvoted_tags", checked)}
                    />
                    包含被降权的标签
                  </label>
                </div>
              </div>

              <div className="space-y-2">
                <label className="text-sm font-medium">禁用过滤器</label>
                <div className="space-y-2">
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={advancedOptions.disable_language_filter ?? false}
                      onCheckedChange={(checked) => updateAdvancedFilter("disable_language_filter", checked)}
                    />
                    禁用语言过滤
                  </label>
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={advancedOptions.disable_uploader_filter ?? false}
                      onCheckedChange={(checked) => updateAdvancedFilter("disable_uploader_filter", checked)}
                    />
                    禁用上传者过滤
                  </label>
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={advancedOptions.disable_tag_filter ?? false}
                      onCheckedChange={(checked) => updateAdvancedFilter("disable_tag_filter", checked)}
                    />
                    禁用标签过滤
                  </label>
                </div>
              </div>
            </div>

            {/* Reset Button */}
            <div className="flex justify-end">
              <Button
                variant="secondary"
                onClick={resetAdvancedFilters}
                disabled={activeFilterCount === 0}
              >
                重置筛选器
              </Button>
            </div>
          </div>
        )}
      </div>

      {/* Results */}
      {isLoading && <GalleryGridSkeleton />}

      {error && (
        <ErrorState
          message={error.message || "搜索失败"}
          onRetry={() => refetch()}
        />
      )}

      {!isLoading && !error && data && data.results.length === 0 && (
        <EmptyState message="没有找到相关 Gallery，请修改搜索条件" />
      )}

      {!isLoading && !error && data && data.results.length > 0 && (
        <div>
          <p className="mb-3 text-sm text-kumo-subtle">
            共找到 {data.total} 个结果
          </p>
          <GalleryGrid galleries={data.results} />
          <SimplePagination
            page={data.page}
            hasMore={(data.page + 1) * data.page_size < data.total}
            onPageChange={handlePageChange}
          />
        </div>
      )}
    </div>
  );
}
