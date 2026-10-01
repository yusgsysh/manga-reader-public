import { useSearchParams } from "react-router";
import { useSearch } from "../hooks/useSearch";
import { useTagTranslation } from "../hooks/useTagTranslation";
import { GalleryGrid } from "../components/gallery/GalleryGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";
import { InfiniteScrollTrigger } from "../components/common/InfiniteScrollTrigger";
import { JumpSeekBar } from "../components/common/JumpSeekBar";
import { Input, Button, Checkbox, Select, cn } from "@cloudflare/kumo";
import {
  MagnifyingGlass,
  CaretDown,
  CaretUp,
  X,
} from "@phosphor-icons/react";
import { useState, useCallback, useRef, useEffect } from "react";
import { PageHeader, Section } from "../components/ui";
import type {
  AdvancedSearchOptions,
  ListingNavOptions,
} from "../types/gallery";

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

const ADVANCED_LABELS: Record<string, string> = {
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

function countActiveFilters(options: AdvancedSearchOptions, tags: string[], pendingTags: string[]): number {
  let count = 0;
  for (const value of Object.values(options)) {
    if (value !== undefined && value !== false && value !== "") count++;
  }
  count += tags.length;
  count += pendingTags.length;
  return count;
}

function FilterChip({
  label,
  onRemove,
}: {
  label: string;
  onRemove: () => void;
}) {
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-[var(--app-chip-bg)] px-2.5 py-1 text-xs font-medium text-[var(--app-chip-fg)]">
      {label}
      <button
        type="button"
        onClick={onRemove}
        aria-label={`移除筛选 ${label}`}
        className="transition-opacity hover:opacity-70"
      >
        <X className="size-3" weight="bold" />
      </button>
    </span>
  );
}

export function SearchPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const q = searchParams.get("q") ?? "";
  const siteParam = searchParams.get("site") ?? "exhentai";
  const categoriesParam = searchParams.get("categories") ?? "";
  const appliedAdvancedOptions = parseAdvancedParams(searchParams);
  const appliedTags = parseTags(searchParams);
  const appliedNav: ListingNavOptions = {
    seek: searchParams.get("seek") ?? undefined,
    jump: searchParams.get("jump") ?? undefined,
  };

  const [inputValue, setInputValue] = useState(q);
  const [site, setSite] = useState(siteParam);
  const [categories, setCategories] = useState(categoriesParam);
  const [pendingAdvancedOptions, setPendingAdvancedOptions] = useState<AdvancedSearchOptions>(appliedAdvancedOptions);
  const [pendingTags, setPendingTags] = useState<string[]>([]);
  const [tagInput, setTagInput] = useState("");
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [showSuggestions, setShowSuggestions] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(-1);

  const tagInputRef = useRef<HTMLInputElement>(null);
  const suggestionsRef = useRef<HTMLDivElement>(null);

  const { searchTags, ready: tagDbReady } = useTagTranslation();
  const [suggestions, setSuggestions] = useState<Array<{ namespace: string; tag: string; translation: string }>>([]);

  const tagQuery = buildTagQuery(appliedTags);
  const fullQuery = [q, tagQuery].filter(Boolean).join(" ");

  const {
    data,
    isLoading,
    error,
    refetch,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = useSearch({
    q: fullQuery || undefined,
    site: siteParam,
    categories: categoriesParam,
    ...appliedAdvancedOptions,
    ...appliedNav,
  });

  const updateAdvancedFilter = (key: keyof AdvancedSearchOptions, value: string | boolean | number | undefined) => {
    setPendingAdvancedOptions((prev) => {
      const next = { ...prev };
      if (value === undefined || value === false || value === "") {
        delete next[key];
      } else {
        (next as Record<string, unknown>)[key] = value;
      }
      return next;
    });
  };

  const handleSearch = () => {
    const params = new URLSearchParams();
    if (inputValue.trim()) params.set("q", inputValue.trim());
    params.set("site", site);
    if (categories) params.set("categories", categories);
    const allTags = [...appliedTags, ...pendingTags];
    if (allTags.length > 0) params.set("tags", [...new Set(allTags)].join(","));
    for (const [key, value] of Object.entries(pendingAdvancedOptions)) {
      if (value !== undefined && value !== false && value !== "") {
        params.set(key, String(value));
      }
    }
    setSearchParams(params);
    setPendingTags([]);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") handleSearch();
  };

  const toggleCategory = (cat: string) => {
    setCategories((prev) => {
      const current = prev ? prev.split(",") : [];
      const next = current.includes(cat)
        ? current.filter((c) => c !== cat)
        : [...current, cat];
      return next.length > 0 ? next.join(",") : "";
    });
  };

  const addPendingTag = (tagValue?: string) => {
    const tag = tagValue ?? tagInput.trim();
    if (!tag) return;
    if (pendingTags.includes(tag) || appliedTags.includes(tag)) return;
    setPendingTags([...pendingTags, tag]);
    setTagInput("");
    setShowSuggestions(false);
    setSelectedIndex(-1);
  };

  const removePendingTag = (tagToRemove: string) => {
    setPendingTags(pendingTags.filter((t) => t !== tagToRemove));
  };

  const removeAppliedTag = (tagToRemove: string) => {
    const newTags = appliedTags.filter((t) => t !== tagToRemove);
    const params = new URLSearchParams(searchParams);
    if (newTags.length > 0) {
      params.set("tags", newTags.join(","));
    } else {
      params.delete("tags");
    }
    setSearchParams(params);
  };

  const removeAppliedOption = (key: string) => {
    const params = new URLSearchParams(searchParams);
    params.delete(key);
    setSearchParams(params);
  };

  const handleNavChange = (next: ListingNavOptions) => {
    const params = new URLSearchParams(searchParams);
    params.delete("seek");
    params.delete("jump");
    if (next.seek) params.set("seek", next.seek);
    if (next.jump) params.set("jump", next.jump);
    setSearchParams(params);
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
    } else if (e.key === "Enter" && selectedIndex >= 0 && suggestions[selectedIndex]) {
      e.preventDefault();
      const s = suggestions[selectedIndex];
      addPendingTag(`${s.namespace}:${s.tag}`);
    }
  };

  const handleTagInputChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const value = e.target.value;
      setTagInput(value);
      setSelectedIndex(-1);

      if (tagDbReady && value.trim().length > 0) {
        const results = searchTags(value.trim(), 6);
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
    setPendingAdvancedOptions({});
    setPendingTags([]);
    setTagInput("");
    setShowSuggestions(false);
  };

  const activeFilterCount = countActiveFilters(pendingAdvancedOptions, appliedTags, pendingTags);
  const activeChips: { key: string; label: string; onRemove: () => void }[] = [
    ...appliedTags.map((tag) => ({
      key: `tag:${tag}`,
      label: tag,
      onRemove: () => removeAppliedTag(tag),
    })),
    ...(appliedNav.seek
      ? [
          {
            key: "seek",
            label: `定位 ${appliedNav.seek}`,
            onRemove: () => removeAppliedOption("seek"),
          },
        ]
      : []),
    ...(appliedNav.jump
      ? [
          {
            key: "jump",
            label: `偏移 ${appliedNav.jump}`,
            onRemove: () => removeAppliedOption("jump"),
          },
        ]
      : []),
    ...Object.entries(appliedAdvancedOptions)
      .filter(([, value]) => value !== undefined && value !== false && value !== "")
      .map(([key, value]) => ({
        key,
        label:
          value === true
            ? (ADVANCED_LABELS[key] ?? key)
            : `${ADVANCED_LABELS[key] ?? key}: ${value}`,
        onRemove: () => removeAppliedOption(key),
      })),
  ];

  const galleries = data?.pages.flatMap((page) => page.results) ?? [];
  const total = data?.pages[0]?.total ?? 0;
  const activeCategories = categories ? categories.split(",") : [];

  return (
    <div className="space-y-5">
      <PageHeader
        title="搜索"
        icon={<MagnifyingGlass className="size-5" weight="bold" />}
      />

      {/* Sticky search bar */}
      <div className="sticky top-14 z-30 -mx-1 bg-kumo-base/85 px-1 py-3 backdrop-blur-md lg:top-0">
        <div className="flex gap-2">
          <Input
            placeholder="搜索 Gallery..."
            aria-label="搜索 Gallery"
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            onKeyDown={handleKeyDown}
            className="flex-1"
          />
          <Button onClick={handleSearch} aria-label="搜索">
            <MagnifyingGlass className="mr-1 size-4" weight="bold" />
            搜索
          </Button>
        </div>
      </div>

      {/* Filters */}
      <div className="card-surface space-y-4 p-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="w-40">
            <Select
              value={site}
              onValueChange={(v) => setSite(v ?? "exhentai")}
              aria-label="Site"
              items={[
                { label: "ExHentai", value: "exhentai" },
                { label: "E-Hentai", value: "ehentai" },
              ]}
            />
          </div>
          <button
            type="button"
            onClick={() => setShowAdvanced(!showAdvanced)}
            className="flex items-center gap-2 text-sm font-medium text-kumo-subtle transition-colors hover:text-kumo-default"
          >
            {showAdvanced ? (
              <CaretUp className="size-4" weight="bold" />
            ) : (
              <CaretDown className="size-4" weight="bold" />
            )}
            高级搜索
            {activeFilterCount > 0 && (
              <span className="inline-flex items-center justify-center rounded-full bg-[var(--app-accent)] px-2 py-0.5 text-xs font-medium text-[var(--app-accent-contrast)]">
                {activeFilterCount}
              </span>
            )}
          </button>
        </div>

        <div className="flex flex-wrap gap-2">
          {CATEGORIES.map((cat) => {
            const active = activeCategories.includes(cat.value);
            return (
              <button
                key={cat.value}
                type="button"
                onClick={() => toggleCategory(cat.value)}
                aria-pressed={active}
                className={cn(
                  "rounded-full border px-3 py-1 text-xs font-medium transition-colors",
                  active
                    ? "border-transparent bg-[var(--app-accent)] text-[var(--app-accent-contrast)]"
                    : "border-kumo-hairline text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-default",
                )}
              >
                {cat.label}
              </button>
            );
          })}
        </div>
      </div>

      <JumpSeekBar
        nav={data?.pages[0]?.nav}
        value={appliedNav}
        onChange={handleNavChange}
        disabled={!fullQuery.trim()}
      />

      {/* Advanced panel */}
      {showAdvanced && (
        <div className="card-surface space-y-6 p-4">
          <Section title="标签">
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
                  className="absolute left-0 right-0 top-full z-10 mt-1 max-h-48 overflow-y-auto rounded-xl border border-kumo-hairline bg-kumo-elevated shadow-lg"
                  role="listbox"
                >
                  {suggestions.map((s, index) => (
                    <button
                      key={`${s.namespace}:${s.tag}`}
                      type="button"
                      role="option"
                      aria-selected={index === selectedIndex}
                      className={cn(
                        "flex w-full items-center justify-between px-3 py-2 text-left text-xs hover:bg-kumo-tint",
                        index === selectedIndex ? "bg-kumo-tint" : "",
                      )}
                      onClick={() => addPendingTag(`${s.namespace}:${s.tag}`)}
                      onMouseEnter={() => setSelectedIndex(index)}
                    >
                      <span className="font-mono text-[11px] text-kumo-subtle">
                        {s.namespace}:{s.tag}
                      </span>
                      <span className="text-[11px]">{s.translation}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>

            {(appliedTags.length > 0 || pendingTags.length > 0) && (
              <div className="mt-2 flex flex-wrap gap-1.5">
                {appliedTags.map((tag) => (
                  <FilterChip
                    key={`applied-${tag}`}
                    label={tag}
                    onRemove={() => removeAppliedTag(tag)}
                  />
                ))}
                {pendingTags.map((tag) => (
                  <span
                    key={`pending-${tag}`}
                    className="inline-flex items-center gap-1 rounded-full bg-kumo-recessed px-2.5 py-1 text-xs font-medium text-kumo-subtle"
                  >
                    {tag}
                    <button
                      type="button"
                      onClick={() => removePendingTag(tag)}
                      aria-label={`移除标签 ${tag}`}
                      className="transition-opacity hover:opacity-70"
                    >
                      <X className="size-3" weight="bold" />
                    </button>
                  </span>
                ))}
              </div>
            )}

            <p className="mt-2 text-xs text-kumo-subtle">
              支持 ExHentai 标签语法：普通标签（yuri）、命名空间标签（female:sole_female）
            </p>
          </Section>

          <Section title="范围">
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <label className="text-xs font-medium text-kumo-subtle">页数范围</label>
                <div className="flex items-center gap-2">
                  <Input
                    type="number"
                    placeholder="最小"
                    aria-label="最小页数"
                    value={pendingAdvancedOptions.min_pages ?? ""}
                    onChange={(e) => {
                      const value = e.target.value ? Number(e.target.value) : undefined;
                      updateAdvancedFilter("min_pages", value);
                    }}
                    min="0"
                  />
                  <span className="text-kumo-subtle">-</span>
                  <Input
                    type="number"
                    placeholder="最大"
                    aria-label="最大页数"
                    value={pendingAdvancedOptions.max_pages ?? ""}
                    onChange={(e) => {
                      const value = e.target.value ? Number(e.target.value) : undefined;
                      updateAdvancedFilter("max_pages", value);
                    }}
                    min="0"
                  />
                </div>
              </div>
              <div className="space-y-2">
                <label className="text-xs font-medium text-kumo-subtle">最低评分</label>
                <Select
                  value={pendingAdvancedOptions.min_rating?.toString() ?? ""}
                  onValueChange={(value) => {
                    updateAdvancedFilter("min_rating", value ? Number(value) : undefined);
                  }}
                  aria-label="最低评分"
                  items={MIN_RATING_OPTIONS}
                />
              </div>
            </div>
          </Section>

          <Section title="筛选选项">
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={pendingAdvancedOptions.has_torrent ?? false}
                  onCheckedChange={(checked) => updateAdvancedFilter("has_torrent", checked)}
                />
                有种子
              </label>
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={pendingAdvancedOptions.include_expunged ?? false}
                  onCheckedChange={(checked) => updateAdvancedFilter("include_expunged", checked)}
                />
                包含已删除的 Gallery
              </label>
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={pendingAdvancedOptions.search_tags ?? false}
                  onCheckedChange={(checked) => updateAdvancedFilter("search_tags", checked)}
                />
                搜索标签
              </label>
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={pendingAdvancedOptions.search_name ?? false}
                  onCheckedChange={(checked) => updateAdvancedFilter("search_name", checked)}
                />
                搜索标题
              </label>
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={pendingAdvancedOptions.search_description ?? false}
                  onCheckedChange={(checked) => updateAdvancedFilter("search_description", checked)}
                />
                搜索描述
              </label>
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={pendingAdvancedOptions.include_low_power_tags ?? false}
                  onCheckedChange={(checked) => updateAdvancedFilter("include_low_power_tags", checked)}
                />
                包含低权重标签
              </label>
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={pendingAdvancedOptions.include_downvoted_tags ?? false}
                  onCheckedChange={(checked) => updateAdvancedFilter("include_downvoted_tags", checked)}
                />
                包含被降权的标签
              </label>
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={pendingAdvancedOptions.disable_language_filter ?? false}
                  onCheckedChange={(checked) => updateAdvancedFilter("disable_language_filter", checked)}
                />
                禁用语言过滤
              </label>
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={pendingAdvancedOptions.disable_uploader_filter ?? false}
                  onCheckedChange={(checked) => updateAdvancedFilter("disable_uploader_filter", checked)}
                />
                禁用上传者过滤
              </label>
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={pendingAdvancedOptions.disable_tag_filter ?? false}
                  onCheckedChange={(checked) => updateAdvancedFilter("disable_tag_filter", checked)}
                />
                禁用标签过滤
              </label>
            </div>
          </Section>

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

      {/* Active filter chips */}
      {activeChips.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-kumo-subtle">已选筛选：</span>
          {activeChips.map((chip) => (
            <FilterChip
              key={chip.key}
              label={chip.label}
              onRemove={chip.onRemove}
            />
          ))}
        </div>
      )}

      {/* Results */}
      {isLoading && <GalleryGridSkeleton />}

      {error && (
        <ErrorState
          message={error.message || "搜索失败"}
          onRetry={() => refetch()}
        />
      )}

      {!isLoading && !error && data && galleries.length === 0 && (
        <EmptyState message="没有找到相关 Gallery，请修改搜索条件" />
      )}

      {!isLoading && !error && galleries.length > 0 && (
        <div>
          <p className="mb-3 text-sm text-kumo-subtle">共找到 {total} 个结果</p>
          <GalleryGrid galleries={galleries} />
          <InfiniteScrollTrigger
            hasNextPage={!!hasNextPage}
            isFetchingNextPage={isFetchingNextPage}
            isFetchNextPageError={isFetchNextPageError}
            fetchNextPage={() => fetchNextPage()}
          />
        </div>
      )}
    </div>
  );
}
