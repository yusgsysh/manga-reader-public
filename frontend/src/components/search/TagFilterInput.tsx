import { useCallback, useEffect, useRef, useState } from "react";
import { Input, cn } from "@cloudflare/kumo";
import { X } from "@phosphor-icons/react";
import { useTagTranslation } from "../../hooks/useTagTranslation";
import { Section } from "../ui";
import { FilterChip } from "./FilterChip";

interface TagFilterInputProps {
  appliedTags?: string[];
  pendingTags: string[];
  onAddTag: (tag: string) => void;
  onRemovePendingTag: (tag: string) => void;
  onRemoveAppliedTag?: (tag: string) => void;
  disabled?: boolean;
}

export function TagFilterInput({
  appliedTags = [],
  pendingTags,
  onAddTag,
  onRemovePendingTag,
  onRemoveAppliedTag,
  disabled,
}: TagFilterInputProps) {
  const [tagInput, setTagInput] = useState("");
  const [showSuggestions, setShowSuggestions] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(-1);
  const [suggestions, setSuggestions] = useState<
    Array<{ namespace: string; tag: string; translation: string }>
  >([]);

  const tagInputRef = useRef<HTMLInputElement>(null);
  const suggestionsRef = useRef<HTMLDivElement>(null);

  const { searchTags, ready: tagDbReady } = useTagTranslation();

  const addTag = (tagValue?: string) => {
    const tag = (tagValue ?? tagInput).trim();
    if (!tag) return;
    if (pendingTags.includes(tag) || appliedTags.includes(tag)) {
      setTagInput("");
      setShowSuggestions(false);
      setSelectedIndex(-1);
      return;
    }
    onAddTag(tag);
    setTagInput("");
    setShowSuggestions(false);
    setSelectedIndex(-1);
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
    } else if (
      e.key === "Enter" &&
      selectedIndex >= 0 &&
      suggestions[selectedIndex]
    ) {
      e.preventDefault();
      const s = suggestions[selectedIndex];
      addTag(`${s.namespace}:${s.tag}`);
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

  return (
    <Section title="标签">
      <div className="relative">
        <Input
          ref={tagInputRef}
          placeholder="输入标签搜索（例如：yuri, 无修正）"
          aria-label="搜索标签"
          aria-autocomplete="list"
          aria-expanded={showSuggestions}
          disabled={disabled}
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
                onClick={() => addTag(`${s.namespace}:${s.tag}`)}
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
              onRemove={() => onRemoveAppliedTag?.(tag)}
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
                onClick={() => onRemovePendingTag(tag)}
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
  );
}
