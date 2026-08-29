import { useSearchParams } from "react-router";
import { useSearch } from "../hooks/useSearch";
import { GalleryGrid } from "../components/gallery/GalleryGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { Pagination } from "../components/common/Pagination";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";
import { Input, Button, Checkbox, Select } from "@cloudflare/kumo";
import { Search } from "lucide-react";
import { useState } from "react";

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

export function SearchPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const q = searchParams.get("q") ?? "";
  const site = searchParams.get("site") ?? "exhentai";
  const categories = searchParams.get("categories") ?? "";
  const page = Number(searchParams.get("page") ?? "0");

  const [inputValue, setInputValue] = useState(q);

  const { data, isLoading, error, refetch } = useSearch({
    q,
    site,
    categories,
    page,
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
    params.set("page", "0");
    setSearchParams(params);
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
          <Pagination
            page={data.page}
            pageSize={data.page_size}
            total={data.total}
            onPageChange={handlePageChange}
          />
        </div>
      )}
    </div>
  );
}
