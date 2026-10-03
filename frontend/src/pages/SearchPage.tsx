import { useSearchParams } from "react-router";
import { useSearch } from "../hooks/useSearch";
import { GalleryGrid } from "../components/gallery/GalleryGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";
import { InfiniteScrollTrigger } from "../components/common/InfiniteScrollTrigger";
import { JumpSeekPanel } from "../components/common/JumpSeekPanel";
import { Input, Button } from "@cloudflare/kumo";
import {
  MagnifyingGlass,
  CaretDown,
  CaretUp,
} from "@phosphor-icons/react";
import { useState } from "react";
import { PageHeader } from "../components/ui";
import { AdvancedFilterFields } from "../components/search/AdvancedFilterFields";
import { TagFilterInput } from "../components/search/TagFilterInput";
import { CategoryFilter } from "../components/search/CategoryFilter";
import {
  ADVANCED_LABELS,
  parseAdvancedParams,
  isActiveValue,
  countActiveFilters,
  serializeAdvancedParams,
} from "../components/search/advancedSearch";
import { FilterChip } from "../components/search/FilterChip";
import {
  ALL_CATEGORY_VALUES,
  normalizeIncludedCategories,
  toggleIncludedCategory,
} from "../lib/categories";
import type {
  AdvancedSearchOptions,
  ListingNavOptions,
} from "../types/gallery";
import { useLastListRoute } from "../hooks/useLastListRoute";

function parseTags(searchParams: URLSearchParams): string[] {
  const tags = searchParams.get("tags");
  if (!tags) return [];
  return tags.split(",").filter(Boolean);
}

function buildTagQuery(tags: string[]): string {
  return tags.map((tag) => `tag:${tag.trim()}`).join(" ");
}

export function SearchPage() {
  useLastListRoute("/search");
  const [searchParams, setSearchParams] = useSearchParams();
  const q = searchParams.get("q") ?? "";
  const categoriesParam = searchParams.get("categories") ?? "";
  const appliedAdvancedOptions = parseAdvancedParams(searchParams);
  const appliedTags = parseTags(searchParams);
  const appliedNav: ListingNavOptions = {
    seek: searchParams.get("seek") ?? undefined,
    jump: searchParams.get("jump") ?? undefined,
  };

  const [inputValue, setInputValue] = useState(q);
  const [categories, setCategories] = useState(categoriesParam);
  const [pendingAdvancedOptions, setPendingAdvancedOptions] = useState<AdvancedSearchOptions>(appliedAdvancedOptions);
  const [pendingTags, setPendingTags] = useState<string[]>([]);
  const [showAdvanced, setShowAdvanced] = useState(false);

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
    site: "exhentai",
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
    if (categories) params.set("categories", categories);
    const allTags = [...appliedTags, ...pendingTags];
    if (allTags.length > 0) params.set("tags", [...new Set(allTags)].join(","));
    serializeAdvancedParams(pendingAdvancedOptions).forEach((value, key) =>
      params.set(key, value),
    );
    setSearchParams(params);
    setPendingTags([]);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") handleSearch();
  };

  // ExHentai semantics: every category is included by default; clicking one
  // excludes it. The `categories` param stores the still-included set, and an
  // empty value means "all included".
  const toggleCategory = (cat: string) => {
    setCategories((prev) => {
      const included = normalizeIncludedCategories(
        prev ? prev.split(",") : undefined,
      );
      const next = toggleIncludedCategory(included, cat);
      return next.length === ALL_CATEGORY_VALUES.length ? "" : next.join(",");
    });
  };

  const addPendingTag = (tag: string) => {
    setPendingTags((prev) => [...prev, tag]);
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

  const resetAdvancedFilters = () => {
    setPendingAdvancedOptions({});
    setPendingTags([]);
  };

  const activeFilterCount =
    countActiveFilters(pendingAdvancedOptions) +
    appliedTags.length +
    pendingTags.length;
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
      .filter(([, value]) => isActiveValue(value))
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
  const includedCategories = normalizeIncludedCategories(
    categories ? categories.split(",") : undefined,
  );

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
            maxLength={200}
            className="flex-1"
          />
          <Button onClick={handleSearch} aria-label="搜索">
            <MagnifyingGlass className="mr-1 size-4" weight="bold" />
            搜索
          </Button>
        </div>
      </div>

      {/* Filters */}
      <div className="card-surface p-4">
        <div className="flex items-center justify-between gap-3">
          <CategoryFilter
            included={includedCategories}
            onToggle={toggleCategory}
            className="min-w-0 flex-1"
          />

          <button
            type="button"
            onClick={() => setShowAdvanced(!showAdvanced)}
            className="flex shrink-0 items-center gap-2 text-sm font-medium text-kumo-subtle transition-colors hover:text-kumo-default"
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
      </div>

      {/* Advanced panel */}
      {showAdvanced && (
        <div className="card-surface space-y-6 p-4">
          <TagFilterInput
            appliedTags={appliedTags}
            pendingTags={pendingTags}
            onAddTag={addPendingTag}
            onRemovePendingTag={removePendingTag}
            onRemoveAppliedTag={removeAppliedTag}
          />

          <AdvancedFilterFields
            value={pendingAdvancedOptions}
            onChange={updateAdvancedFilter}
          />

          <JumpSeekPanel
            nav={data?.pages[0]?.nav}
            value={appliedNav}
            onChange={handleNavChange}
          />

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
