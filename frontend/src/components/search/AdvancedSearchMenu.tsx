import { useState } from "react";
import { Button, Popover } from "@cloudflare/kumo";
import { Funnel } from "@phosphor-icons/react";
import { IconButton, Section } from "../ui";
import { AdvancedFilterFields } from "./AdvancedFilterFields";
import { TagFilterInput } from "./TagFilterInput";
import { CategoryFilter } from "./CategoryFilter";
import { countActiveFilters, pickAdvancedOptions } from "./advancedSearch";
import {
  ALL_CATEGORY_VALUES,
  normalizeIncludedCategories,
  toggleIncludedCategory,
} from "../../lib/categories";
import type {
  AdvancedSearchOptions,
  GalleryListFilters,
} from "../../types/gallery";

interface AdvancedSearchMenuProps {
  value: GalleryListFilters;
  onApply: (value: GalleryListFilters) => void;
  disabled?: boolean;
}

function countFilters(filters: GalleryListFilters): number {
  const excluded =
    ALL_CATEGORY_VALUES.length -
    normalizeIncludedCategories(filters.categories).length;
  return (
    countActiveFilters(filters) + (filters.tags?.length ?? 0) + excluded
  );
}

export function AdvancedSearchMenu({
  value,
  onApply,
  disabled,
}: AdvancedSearchMenuProps) {
  const [open, setOpen] = useState(false);
  const [pendingOptions, setPendingOptions] = useState<AdvancedSearchOptions>(
    () => pickAdvancedOptions(value),
  );
  const [pendingTags, setPendingTags] = useState<string[]>(value.tags ?? []);
  const [pendingCategories, setPendingCategories] = useState<string[]>(() =>
    normalizeIncludedCategories(value.categories),
  );

  const handleOpenChange = (next: boolean) => {
    if (next) {
      setPendingOptions(pickAdvancedOptions(value));
      setPendingTags(value.tags ?? []);
      setPendingCategories(normalizeIncludedCategories(value.categories));
    }
    setOpen(next);
  };

  const updateOption = (
    key: keyof AdvancedSearchOptions,
    next: string | boolean | number | undefined,
  ) => {
    setPendingOptions((prev) => {
      const copy = { ...prev };
      if (next === undefined || next === false || next === "") {
        delete copy[key];
      } else {
        (copy as Record<string, unknown>)[key] = next;
      }
      return copy;
    });
  };

  const handleApply = () => {
    const next: GalleryListFilters = { ...pendingOptions };
    if (pendingTags.length > 0) next.tags = pendingTags;
    if (pendingCategories.length < ALL_CATEGORY_VALUES.length) {
      next.categories = pendingCategories;
    }
    onApply(next);
    setOpen(false);
  };

  const handleReset = () => {
    setPendingOptions({});
    setPendingTags([]);
    setPendingCategories([...ALL_CATEGORY_VALUES]);
  };

  const activeCount = countFilters(value);
  const active = activeCount > 0;

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <Popover.Trigger
        disabled={disabled}
        render={
          <IconButton
            label={active ? `高级搜索（${activeCount}）` : "高级搜索"}
            size="sm"
            disabled={disabled}
            className={cnActive(active)}
          >
            <Funnel className="size-4" weight="bold" />
            {activeCount > 0 && (
              <span className="absolute -right-1 -top-1 flex min-w-4 items-center justify-center rounded-full bg-[var(--app-accent)] px-1 text-[10px] font-medium leading-4 text-[var(--app-accent-contrast)]">
                {activeCount}
              </span>
            )}
          </IconButton>
        }
      />
      <Popover.Content
        side="bottom"
        align="end"
        sideOffset={8}
        className="max-h-[70vh] w-[min(94vw,30rem)] overflow-y-auto p-4"
      >
        <div className="space-y-6">
          <Section title="分类">
            <CategoryFilter
              included={pendingCategories}
              onToggle={(category) =>
                setPendingCategories((prev) =>
                  toggleIncludedCategory(prev, category),
                )
              }
              disabled={disabled}
            />
          </Section>

          <TagFilterInput
            pendingTags={pendingTags}
            onAddTag={(tag) => setPendingTags((prev) => [...prev, tag])}
            onRemovePendingTag={(tag) =>
              setPendingTags((prev) => prev.filter((t) => t !== tag))
            }
            disabled={disabled}
          />

          <AdvancedFilterFields
            value={pendingOptions}
            onChange={updateOption}
            disabled={disabled}
            layout="stacked"
          />

          <div className="flex justify-end gap-2">
            <Button
              variant="ghost"
              onClick={handleReset}
              disabled={disabled}
            >
              重置
            </Button>
            <Button onClick={handleApply} disabled={disabled}>
              应用
            </Button>
          </div>
        </div>
      </Popover.Content>
    </Popover>
  );
}

function cnActive(active: boolean): string | undefined {
  return active
    ? "relative bg-[var(--app-accent-soft)] text-[var(--app-accent)] hover:bg-[var(--app-accent-soft)] hover:text-[var(--app-accent)]"
    : "relative";
}
