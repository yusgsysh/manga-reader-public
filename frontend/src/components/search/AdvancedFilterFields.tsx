import { Input, Checkbox, Select, cn } from "@cloudflare/kumo";
import { Section } from "../ui";
import { MIN_RATING_OPTIONS, normalizePageBound } from "./advancedSearch";
import type { AdvancedSearchOptions } from "../../types/gallery";

type AdvancedKey = keyof AdvancedSearchOptions;

interface FilterToggle {
  key: AdvancedKey;
  label: string;
}

// Mirrors the ExHentai advanced pane's first group: Expunged / Torrent.
const PRIMARY_TOGGLES: FilterToggle[] = [
  { key: "include_expunged", label: "包含已删除的 Gallery" },
  { key: "has_torrent", label: "有种子" },
];

// Mirrors the ExHentai advanced pane's "Disable custom filters for" group.
const DISABLE_FILTER_TOGGLES: FilterToggle[] = [
  { key: "disable_language_filter", label: "禁用语言过滤" },
  { key: "disable_uploader_filter", label: "禁用上传者过滤" },
  { key: "disable_tag_filter", label: "禁用标签过滤" },
];

interface AdvancedFilterFieldsProps {
  value: AdvancedSearchOptions;
  onChange: (
    key: keyof AdvancedSearchOptions,
    value: string | boolean | number | undefined,
  ) => void;
  disabled?: boolean;
  /**
   * `"responsive"` uses viewport breakpoints (inline search panel).
   * `"stacked"` keeps a single-column range and 2-column toggles that fit the
   * narrow popover without relying on the viewport width.
   */
  layout?: "responsive" | "stacked";
}

export function AdvancedFilterFields({
  value,
  onChange,
  disabled,
  layout = "responsive",
}: AdvancedFilterFieldsProps) {
  const toggleGrid = cn(
    "grid gap-3",
    layout === "stacked" ? "grid-cols-2" : "sm:grid-cols-2 lg:grid-cols-3",
  );

  const renderToggles = (toggles: FilterToggle[]) => (
    <div className={toggleGrid}>
      {toggles.map((toggle) => (
        <label key={toggle.key} className="flex items-center gap-2 text-sm">
          <Checkbox
            checked={Boolean(value[toggle.key])}
            disabled={disabled}
            onCheckedChange={(checked) => onChange(toggle.key, checked)}
          />
          {toggle.label}
        </label>
      ))}
    </div>
  );

  return (
    <>
      <Section title="筛选选项">{renderToggles(PRIMARY_TOGGLES)}</Section>

      <Section title="范围">
        <div
          className={cn(
            "grid grid-cols-1 gap-4",
            layout === "responsive" && "sm:grid-cols-2",
          )}
        >
          <div className="space-y-3">
            <label className="block text-xs font-medium text-kumo-subtle">
              页数范围
            </label>
            <div className="flex items-center gap-2">
              <Input
                type="number"
                placeholder="最小"
                aria-label="最小页数"
                disabled={disabled}
                className="min-w-0 flex-1"
                value={value.min_pages ?? ""}
                onChange={(e) =>
                  onChange("min_pages", normalizePageBound(e.target.value))
                }
                min="1"
                max="9999"
              />
              <span className="shrink-0 text-kumo-subtle">-</span>
              <Input
                type="number"
                placeholder="最大"
                aria-label="最大页数"
                disabled={disabled}
                className="min-w-0 flex-1"
                value={value.max_pages ?? ""}
                onChange={(e) =>
                  onChange("max_pages", normalizePageBound(e.target.value))
                }
                min="1"
                max="9999"
              />
            </div>
          </div>
          <div className="space-y-3">
            <label className="block text-xs font-medium text-kumo-subtle">
              最低评分
            </label>
            <Select
              value={value.min_rating?.toString() ?? ""}
              onValueChange={(next) => {
                onChange("min_rating", next ? Number(next) : undefined);
              }}
              aria-label="最低评分"
              disabled={disabled}
              className="w-full"
              items={MIN_RATING_OPTIONS}
            />
          </div>
        </div>
      </Section>

      <Section title="禁用自定义过滤器">
        {renderToggles(DISABLE_FILTER_TOGGLES)}
      </Section>
    </>
  );
}
