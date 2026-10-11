import { useMemo, useState } from "react";
import {
  ArrowsLeftRight,
  CircleNotch,
  Heart,
  Tag,
  X,
} from "@phosphor-icons/react";
import { Input, useKumoToastManager, cn } from "@cloudflare/kumo";
import { useDocumentTitle } from "../hooks/useDocumentTitle";
import {
  useTagPreferences,
  useSaveTagPreferences,
  TAG_ACTION_TEXT_CLASS,
  type TagAction,
} from "../hooks/useTagPreferences";
import { useTagTranslation } from "../hooks/useTagTranslation";
import { PageHeader, Section } from "../components/ui";

type TagSuggestion = { namespace: string; tag: string; translation: string };

/* Autocomplete input for picking a tag from the translation database. */
function TagPicker({
  placeholder,
  onPick,
  exclude,
}: {
  placeholder: string;
  onPick: (tag: string) => void;
  exclude: ReadonlySet<string>;
}) {
  const { searchTags, ready } = useTagTranslation();
  const [input, setInput] = useState("");
  const [results, setResults] = useState<TagSuggestion[]>([]);
  const [dismissed, setDismissed] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(-1);

  // Mirrors TagFilterInput: search on each keystroke against the in-memory
  // translation DB (scanning ~44k entries is cheap enough synchronously once
  // debounced by the input event rate; the DB service caches results).
  const handleInput = (value: string) => {
    setInput(value);
    setSelectedIndex(-1);
    setDismissed(false);
    const q = value.trim();
    if (!ready || q.length === 0) {
      setResults([]);
      return;
    }
    setResults(searchTags(q, 8));
  };

  const suggestions = useMemo(
    () =>
      dismissed
        ? []
        : results.filter((s) => !exclude.has(`${s.namespace}:${s.tag}`)),
    [results, dismissed, exclude],
  );

  const pick = (s: TagSuggestion) => {
    onPick(`${s.namespace}:${s.tag}`);
    setInput("");
    setResults([]);
    setDismissed(true);
    setSelectedIndex(-1);
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelectedIndex((prev) => Math.min(prev + 1, suggestions.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelectedIndex((prev) => Math.max(prev - 1, -1));
    } else if (e.key === "Escape") {
      setDismissed(true);
      setSelectedIndex(-1);
    } else if (
      e.key === "Enter" &&
      selectedIndex >= 0 &&
      suggestions[selectedIndex]
    ) {
      e.preventDefault();
      pick(suggestions[selectedIndex]);
    }
  };

  return (
    <div className="relative">
      <Input
        placeholder={placeholder}
        aria-label={placeholder}
        aria-autocomplete="list"
        aria-expanded={suggestions.length > 0}
        value={input}
        onChange={(e) => handleInput(e.target.value)}
        onKeyDown={onKeyDown}
        onFocus={() => setDismissed(false)}
        className="w-full"
      />
      {suggestions.length > 0 && (
        <div
          className="absolute left-0 right-0 top-full z-10 mt-1 max-h-56 overflow-y-auto rounded-xl border border-kumo-hairline bg-kumo-elevated shadow-lg"
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
              onClick={() => pick(s)}
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
  );
}

/* One list (liked / disliked) with its add-picker and current rules. */
function PreferenceList({
  action,
  rules,
  busy,
  onAdd,
  onRemove,
  onFlip,
}: {
  action: TagAction;
  rules: string[];
  busy: boolean;
  onAdd: (tag: string) => void;
  onRemove: (tag: string) => void;
  onFlip: (tag: string) => void;
}) {
  const { translateTag, translateNamespace, hasTranslation } =
    useTagTranslation();
  const isLike = action === "like";
  const accentClass = TAG_ACTION_TEXT_CLASS[action];
  const exclude = useMemo(() => new Set(rules), [rules]);

  return (
    <Section
      title={
        <span className="inline-flex items-center gap-1.5">
          {isLike ? (
            <Heart className="size-4 text-blue-500" weight="fill" />
          ) : (
            <X className="size-4 text-red-500" weight="bold" />
          )}
          {isLike ? "喜欢的标签" : "不喜欢的标签"}
          <span className="text-xs font-normal text-kumo-subtle">
            {isLike ? "显示为蓝色" : "显示为红色"}
          </span>
        </span>
      }
    >
      <div className="space-y-3">
        <TagPicker
          placeholder={
            isLike ? "搜索并添加喜欢的标签" : "搜索并添加不喜欢的标签"
          }
          onPick={onAdd}
          exclude={exclude}
        />
        {rules.length === 0 ? (
          <p className="text-sm text-kumo-subtle">
            {isLike ? "还没有喜欢的标签" : "还没有不喜欢的标签"}
          </p>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {rules.map((raw) => {
              const idx = raw.indexOf(":");
              const ns = idx > 0 ? raw.slice(0, idx) : "";
              const name = idx > 0 ? raw.slice(idx + 1) : raw;
              const tag = { namespace: ns, name };
              const label = hasTranslation(tag) ? translateTag(tag) : name;
              const nsLabel = ns ? translateNamespace(ns) : "";
              return (
                <span
                  key={raw}
                  className={cn(
                    "inline-flex items-center gap-1 rounded-full bg-kumo-recessed px-2.5 py-1 text-xs font-medium",
                    accentClass,
                  )}
                >
                  {nsLabel && <span className="opacity-70">[{nsLabel}]</span>}
                  <span>{label}</span>
                  <button
                    type="button"
                    onClick={() => onFlip(raw)}
                    disabled={busy}
                    title={isLike ? "改为不喜欢" : "改为喜欢"}
                    aria-label={
                      isLike ? `将 ${raw} 改为不喜欢` : `将 ${raw} 改为喜欢`
                    }
                    className="ml-0.5 rounded-full p-0.5 transition-opacity hover:opacity-70 disabled:opacity-40"
                  >
                    <ArrowsLeftRight className="size-3" weight="bold" />
                  </button>
                  <button
                    type="button"
                    onClick={() => onRemove(raw)}
                    disabled={busy}
                    aria-label={`移除 ${raw}`}
                    className="rounded-full p-0.5 transition-opacity hover:opacity-70 disabled:opacity-40"
                  >
                    <X className="size-3" weight="bold" />
                  </button>
                </span>
              );
            })}
          </div>
        )}
      </div>
    </Section>
  );
}

export function TagSettingsPage() {
  useDocumentTitle("标签设置");
  const toast = useKumoToastManager();
  const { preferences, isLoading, isError } = useTagPreferences();
  const saveMutation = useSaveTagPreferences();

  const [likeTags, setLikeTags] = useState<string[]>([]);
  const [dislikeTags, setDislikeTags] = useState<string[]>([]);
  const [seeded, setSeeded] = useState(false);

  // Adopt server state on first load (adjust-state-during-render pattern,
  // same as SyncSection in SettingsPage); local edits take over afterwards.
  if (!seeded && !isLoading && !isError) {
    setSeeded(true);
    setLikeTags(
      preferences.filter((p) => p.action === "like").map((p) => p.tag),
    );
    setDislikeTags(
      preferences.filter((p) => p.action === "dislike").map((p) => p.tag),
    );
  }

  const busy = saveMutation.isPending;

  const persist = (nextLike: string[], nextDislike: string[]) => {
    const tags = [
      ...nextLike.map((tag) => ({ tag, action: "like" as TagAction })),
      ...nextDislike.map((tag) => ({ tag, action: "dislike" as TagAction })),
    ];
    saveMutation.mutate(
      { tags },
      {
        onSuccess: (data) => {
          setLikeTags(
            data.tags.filter((p) => p.action === "like").map((p) => p.tag),
          );
          setDislikeTags(
            data.tags.filter((p) => p.action === "dislike").map((p) => p.tag),
          );
        },
        onError: (error) => {
          toast.add({
            title: "保存标签设置失败",
            description: error instanceof Error ? error.message : undefined,
            variant: "error",
          });
        },
      },
    );
  };

  const handleAdd = (action: TagAction) => (tag: string) => {
    const key = tag.toLowerCase();
    if (action === "like") {
      if (likeTags.includes(key) || dislikeTags.includes(key)) return;
      persist([key, ...likeTags], dislikeTags);
    } else {
      if (dislikeTags.includes(key) || likeTags.includes(key)) return;
      persist(likeTags, [key, ...dislikeTags]);
    }
  };

  const handleRemove = (action: TagAction) => (tag: string) => {
    if (action === "like") {
      persist(
        likeTags.filter((t) => t !== tag),
        dislikeTags,
      );
    } else {
      persist(
        likeTags,
        dislikeTags.filter((t) => t !== tag),
      );
    }
  };

  // Move a tag to the opposite list (switches its colour).
  const handleFlip = (action: TagAction) => (tag: string) => {
    if (action === "like") {
      persist(
        likeTags.filter((t) => t !== tag),
        [tag, ...dislikeTags],
      );
    } else {
      persist(
        [tag, ...likeTags],
        dislikeTags.filter((t) => t !== tag),
      );
    }
  };

  return (
    <div className="max-w-3xl">
      <PageHeader
        title="标签设置"
        icon={<Tag className="size-5" weight="fill" />}
      />
      <p className="mb-6 text-sm text-kumo-subtle">
        给标签标记喜欢或不喜欢：喜欢的标签在详情页、卡片浮窗和搜索里显示为{" "}
        <span className="font-medium text-blue-600 dark:text-blue-400">蓝色</span>
        ，不喜欢的显示为{" "}
        <span className="font-medium text-red-600 dark:text-red-400">红色</span>
        ，并排在中性标签前面。未标记的标签保持原样。只影响颜色，不过滤内容。
      </p>

      {isLoading ? (
        <div className="flex items-center gap-2 text-kumo-subtle">
          <CircleNotch className="size-4 animate-spin" />
          正在读取标签设置…
        </div>
      ) : isError ? (
        <p className="text-sm text-kumo-subtle">
          无法读取标签设置，请稍后重试。
        </p>
      ) : (
        <div className="space-y-8">
          <PreferenceList
            action="like"
            rules={likeTags}
            busy={busy}
            onAdd={handleAdd("like")}
            onRemove={handleRemove("like")}
            onFlip={handleFlip("like")}
          />
          <PreferenceList
            action="dislike"
            rules={dislikeTags}
            busy={busy}
            onAdd={handleAdd("dislike")}
            onRemove={handleRemove("dislike")}
            onFlip={handleFlip("dislike")}
          />
        </div>
      )}
    </div>
  );
}
