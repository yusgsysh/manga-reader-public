import { useEffect, useMemo, useRef, useState } from "react";
import { Button, Dialog, Input, cn } from "@cloudflare/kumo";
import { CircleNotch, Tag, X } from "@phosphor-icons/react";
import { useDocumentTitle } from "../hooks/useDocumentTitle";
import {
  useTagPreferences,
  useSaveTagPreferences,
  useSyncTagPreferences,
  TAG_ACTION_CHIP_CLASS,
  type TagAction,
} from "../hooks/useTagPreferences";
import type {
  SyncTagSettingsOptions,
  TagSyncConflict,
  TagSyncResult,
  TagSyncSide,
} from "../api/tagSettings";
import { useTagTranslation } from "../hooks/useTagTranslation";
import { PageHeader, Section, useToast } from "../components/ui";

type TagSuggestion = { namespace: string; tag: string; translation: string };

/** One editable rule row, mirroring source My Tags (action + weight). */
interface LocalRule {
  tag: string;
  action: TagAction;
  weight: number;
}

/** Source My Tags defaults: like +10, dislike -10. */
const DEFAULT_WEIGHT: Record<TagAction, number> = { like: 10, dislike: -10 };

/** Weight bounds mirror the source input (3 digits + sign). */
const MIN_WEIGHT = -999;
const MAX_WEIGHT = 999;

/** Display order: likes (strongest first), then dislikes (strongest first). */
function sortRuleRows(rules: LocalRule[]): LocalRule[] {
  const rank = (r: LocalRule) => (r.action === "like" ? 0 : 1);
  return [...rules].sort((a, b) => {
    const d = rank(a) - rank(b);
    if (d !== 0) return d;
    return Math.abs(b.weight) - Math.abs(a.weight);
  });
}

/** Parse a weight input; null means invalid (revert the field). */
function parseWeightInput(raw: string): number | null {
  const trimmed = raw.trim();
  if (trimmed === "") return null;
  const n = Number.parseInt(trimmed, 10);
  if (Number.isNaN(n) || String(n) !== trimmed) return null;
  if (n < MIN_WEIGHT || n > MAX_WEIGHT) return null;
  return n;
}

/* Autocomplete input for picking a tag from the translation database. */
function TagPicker({
  placeholder,
  onPick,
  exclude,
  disabled = false,
}: {
  placeholder: string;
  onPick: (tag: string) => void;
  exclude: ReadonlySet<string>;
  disabled?: boolean;
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
        disabled={disabled}
        onChange={(e) => handleInput(e.target.value)}
        onKeyDown={onKeyDown}
        onFocus={() => setDismissed(false)}
        className="w-full"
      />
      {suggestions.length > 0 && !disabled && (
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

/* One rule row: preview chip | 喜欢 ☐ | 不喜欢 ☐ | weight | ✕. */
function RuleRow({
  rule,
  busy,
  onActionChange,
  onWeightCommit,
  onRemove,
}: {
  rule: LocalRule;
  busy: boolean;
  onActionChange: (tag: string, action: TagAction) => void;
  onWeightCommit: (tag: string, weight: number) => void;
  onRemove: (tag: string) => void;
}) {
  const { translateTag, translateNamespace, hasTranslation } =
    useTagTranslation();

  const idx = rule.tag.indexOf(":");
  const ns = idx > 0 ? rule.tag.slice(0, idx) : "";
  const name = idx > 0 ? rule.tag.slice(idx + 1) : rule.tag;
  const tagObj = { namespace: ns, name };
  const label = hasTranslation(tagObj) ? translateTag(tagObj) : name;
  const nsLabel = ns ? translateNamespace(ns) : "";

  const isLike = rule.action === "like";
  const chipClass = TAG_ACTION_CHIP_CLASS[rule.action];

  const commitWeight = (raw: string) => {
    const parsed = parseWeightInput(raw);
    if (parsed === null || parsed === rule.weight) return; // revert via key remount
    onWeightCommit(rule.tag, parsed);
  };

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-xl border border-kumo-hairline px-3 py-2">
      <span
        className={cn(
          "inline-flex min-w-0 max-w-[16rem] items-center gap-1 rounded-md px-1.5 py-0.5 text-xs font-medium",
          chipClass,
        )}
      >
        {nsLabel && <span className="opacity-70">[{nsLabel}]</span>}
        <span className="truncate">{label}</span>
      </span>

      <label className="inline-flex cursor-pointer items-center gap-1.5 text-xs">
        <input
          type="checkbox"
          checked={isLike}
          disabled={busy}
          onChange={() => onActionChange(rule.tag, "like")}
          aria-label={`将 ${rule.tag} 标为喜欢`}
          className="accent-blue-500"
        />
        喜欢
      </label>
      <label className="inline-flex cursor-pointer items-center gap-1.5 text-xs">
        <input
          type="checkbox"
          checked={!isLike}
          disabled={busy}
          onChange={() => onActionChange(rule.tag, "dislike")}
          aria-label={`将 ${rule.tag} 标为不喜欢`}
          className="accent-red-500"
        />
        不喜欢
      </label>

      <label className="inline-flex items-center gap-1.5 text-xs text-kumo-subtle">
        权重
        <Input
          // Remount when the stored weight changes so a cancelled edit snaps back.
          key={`${rule.tag}-${rule.weight}`}
          type="number"
          defaultValue={rule.weight}
          min={MIN_WEIGHT}
          max={MAX_WEIGHT}
          disabled={busy}
          aria-label={`${rule.tag} 权重`}
          onBlur={(e) => commitWeight(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              commitWeight((e.target as HTMLInputElement).value);
            }
          }}
          className="w-16"
        />
      </label>

      <button
        type="button"
        onClick={() => onRemove(rule.tag)}
        disabled={busy}
        aria-label={`删除 ${rule.tag}`}
        className="ml-auto rounded-full p-1 text-kumo-subtle transition-opacity hover:opacity-70 disabled:opacity-40"
      >
        <X className="size-4" weight="bold" />
      </button>
    </div>
  );
}

/* One side of a conflict (本地/源站): solid action chip or 已删除. */
function ConflictSide({ label, side }: { label: string; side: TagSyncSide }) {
  return (
    <div className="flex min-w-0 items-center gap-1.5 rounded bg-kumo-recessed px-2 py-1">
      <span className="shrink-0 text-[11px] text-kumo-subtle">{label}</span>
      {side === null ? (
        <span className="text-[11px] text-kumo-subtle">已删除</span>
      ) : (
        <span
          className={cn(
            "truncate rounded px-1.5 py-0.5 text-[11px] font-medium",
            TAG_ACTION_CHIP_CLASS[side.action],
          )}
        >
          {side.action === "like" ? "喜欢" : "不喜欢"}
          {side.action === "like" && side.weight >= 0 ? ` +${side.weight}` : ` ${side.weight}`}
        </span>
      )}
    </div>
  );
}

export function TagSettingsPage() {
  useDocumentTitle("标签设置");
  const toast = useToast();
  const { preferences, isLoading, isError } = useTagPreferences();
  const saveMutation = useSaveTagPreferences();
  const syncMutation = useSyncTagPreferences();

  const [rules, setRules] = useState<LocalRule[]>([]);
  const [seeded, setSeeded] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [pendingDeletes, setPendingDeletes] = useState<string[] | null>(null);
  const [deleteDismissed, setDeleteDismissed] = useState(false);
  const [conflicts, setConflicts] = useState<TagSyncConflict[] | null>(null);

  const syncQueuedRef = useRef<SyncTagSettingsOptions | null>(null);
  const syncStartedRef = useRef(false);
  const stashedPendingRef = useRef<string[] | null>(null);
  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  // Adopt server state on first load (adjust-state-during-render pattern,
  // same as SyncSection in SettingsPage); local edits take over afterwards.
  if (!seeded && !isLoading && !isError) {
    setSeeded(true);
    setRules(
      preferences.map((p) => ({
        tag: p.tag,
        action: p.action,
        weight: p.weight ?? DEFAULT_WEIGHT[p.action],
      })),
    );
  }

  // Inputs lock while saving or syncing so a pull can't race a local edit.
  const busy = saveMutation.isPending || syncMutation.isPending;
  const exclude = useMemo(() => new Set(rules.map((r) => r.tag)), [rules]);
  const sortedRules = useMemo(() => sortRuleRows(rules), [rules]);

  const rulesFromTags = (tags: { tag: string; action: TagAction; weight: number }[]) =>
    tags.map((p) => ({
      tag: p.tag,
      action: p.action,
      weight: p.weight ?? DEFAULT_WEIGHT[p.action],
    }));

  // Local edits only; nothing hits the server until 保存 is pressed.
  const updateRules = (next: LocalRule[]) => {
    setRules(next);
    setDirty(true);
  };

  const handleSyncSuccess = (result: TagSyncResult) => {
    if (!mountedRef.current) return;
    if (result.failed.length > 0) {
      const first = result.failed[0];
      toast.error(
        "部分标签同步失败",
        `${result.failed.length} 个标签未能同步到源站（${first.tag}: ${first.error}）`,
      );
    }
    // Pulled source changes first: adopt them into the editable list
    // (the hook has already reseeded the shared query cache).
    if (result.pulled > 0 && result.tags.length > 0) {
      setRules(rulesFromTags(result.tags));
      setDirty(false);
      toast.info(
        "已从源站拉取变更",
        `${result.pulled} 个标签已按源站 My Tags 更新到本地`,
      );
    }
    if (result.conflicts.length > 0) {
      // Show the conflict picker; any pending deletions are re-reported by
      // the resolution sync, or replayed when the dialog is dismissed.
      stashedPendingRef.current =
        result.pending_deletes.length > 0 ? result.pending_deletes : null;
      setConflicts(result.conflicts);
      return;
    }
    if (result.pending_deletes.length > 0) {
      if (!deleteDismissed) {
        setPendingDeletes(result.pending_deletes);
      } else {
        toast.info(
          "部分源站标签未删除",
          `${result.pending_deletes.length} 个源站标签在本地已不存在，需确认后才会从源站删除`,
        );
      }
    }
    const changed = result.added + result.updated + result.deleted;
    if (changed > 0) {
      toast.success(
        "已同步到源站",
        `新增 ${result.added} · 更新 ${result.updated} · 删除 ${result.deleted}`,
      );
    }
  };

  const requestSync = (options: SyncTagSettingsOptions) => {
    if (syncMutation.isPending) {
      // Coalesce queued requests, keeping confirmation/resolution flags.
      const prev = syncQueuedRef.current;
      syncQueuedRef.current = {
        confirmDeletes: prev?.confirmDeletes || options.confirmDeletes || false,
        conflictResolution:
          options.conflictResolution ?? prev?.conflictResolution,
      };
      return;
    }
    syncMutation.mutate(options, {
      onSuccess: handleSyncSuccess,
      onError: (error) => {
        if (!mountedRef.current) return;
        toast.error("同步源站失败", error);
      },
      onSettled: () => {
        if (!mountedRef.current) return;
        const queued = syncQueuedRef.current;
        if (queued) {
          syncQueuedRef.current = null;
          requestSync(queued);
        }
      },
    });
  };

  // Full sync on page open: pull source changes, push local ones, report conflicts.
  useEffect(() => {
    if (!seeded || isError || syncStartedRef.current) return;
    syncStartedRef.current = true;
    requestSync({});
  });

  // Explicit save: write the list, then sync it to source My Tags.
  const handleSave = () => {
    saveMutation.mutate(
      { tags: rules.map((r) => ({ tag: r.tag, action: r.action, weight: r.weight })) },
      {
        onSuccess: (data) => {
          setRules(rulesFromTags(data.tags));
          setDirty(false);
          requestSync({});
        },
        onError: (error) => {
          toast.error("保存标签设置失败", error);
        },
      },
    );
  };

  const handleAdd = (rawTag: string) => {
    const tag = rawTag.toLowerCase();
    if (rules.some((r) => r.tag === tag)) return;
    updateRules([...rules, { tag, action: "like", weight: DEFAULT_WEIGHT.like }]);
  };

  // Unchecking the active action removes the rule; checking the other action
  // flips it, preserving the weight magnitude (sign follows the action).
  const handleActionChange = (tag: string, action: TagAction) => {
    const existing = rules.find((r) => r.tag === tag);
    if (!existing) return;
    if (existing.action === action) {
      updateRules(rules.filter((r) => r.tag !== tag));
      return;
    }
    const flippedWeight =
      existing.weight === 0 ? 0 : -existing.weight;
    updateRules(
      rules.map((r) =>
        r.tag === tag ? { ...r, action, weight: flippedWeight } : r,
      ),
    );
  };

  const handleWeightCommit = (tag: string, weight: number) => {
    updateRules(rules.map((r) => (r.tag === tag ? { ...r, weight } : r)));
  };

  const handleRemove = (tag: string) => {
    updateRules(rules.filter((r) => r.tag !== tag));
  };

  const confirmPendingDeletes = () => {
    setPendingDeletes(null);
    setDeleteDismissed(true);
    requestSync({ confirmDeletes: true });
  };

  // Dismissing the conflict dialog (取消): replay any deferred deletions.
  const closeConflictDialog = () => {
    setConflicts(null);
    const stashed = stashedPendingRef.current;
    stashedPendingRef.current = null;
    if (stashed) setPendingDeletes(stashed);
  };

  // One choice resolves every listed conflict: re-sync with the resolution.
  const resolveConflicts = (resolution: "remote" | "local") => {
    stashedPendingRef.current = null;
    setConflicts(null);
    requestSync({ conflictResolution: resolution });
  };

  return (
    <div className="max-w-3xl">
      <PageHeader
        title="标签设置"
        icon={<Tag className="size-5" weight="fill" />}
        actions={
          <>
            {dirty && (
              <span className="text-xs text-kumo-subtle">有未保存的修改</span>
            )}
            <Button onClick={handleSave} disabled={!dirty || busy}>
              {saveMutation.isPending && (
                <CircleNotch className="mr-1 size-4 animate-spin" />
              )}
              保存
            </Button>
          </>
        }
      />

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
        <Section
          title={
            <span className="inline-flex items-center gap-2">
              标签规则
              {syncMutation.isPending && (
                <span className="inline-flex items-center gap-1 text-xs font-normal text-kumo-subtle">
                  <CircleNotch className="size-3 animate-spin" />
                  正在同步源站…
                </span>
              )}
            </span>
          }
          description="喜欢/不喜欢互斥；点已勾选的框可删除该规则。"
        >
          <div className="space-y-3">
            <TagPicker
              placeholder="搜索并添加标签（默认：喜欢 +10）"
              onPick={handleAdd}
              exclude={exclude}
              disabled={busy}
            />
            {sortedRules.length === 0 ? (
              <p className="text-sm text-kumo-subtle">还没有标签规则</p>
            ) : (
              <div className="space-y-2">
                {sortedRules.map((rule) => (
                  <RuleRow
                    key={rule.tag}
                    rule={rule}
                    busy={busy}
                    onActionChange={handleActionChange}
                    onWeightCommit={handleWeightCommit}
                    onRemove={handleRemove}
                  />
                ))}
              </div>
            )}
          </div>
        </Section>
      )}

      <Dialog.Root
        open={conflicts !== null}
        onOpenChange={(open) => {
          if (!open) closeConflictDialog();
        }}
      >
        <Dialog className="w-[min(92vw,32rem)] p-6">
          <Dialog.Title className="text-base font-semibold">
            同步冲突：保留哪一边？
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-kumo-subtle">
            {conflicts?.length ?? 0} 个标签在本地和源站都被改过，本次同步先
            暂不改动它们。统一选择要保留的一边：
          </Dialog.Description>
          <ul className="mt-3 max-h-64 space-y-2 overflow-y-auto">
            {conflicts?.map((conflict) => (
              <li
                key={conflict.tag}
                className="rounded-lg border border-kumo-hairline px-2.5 py-2"
              >
                <div className="truncate font-mono text-[11px]">
                  {conflict.tag}
                </div>
                <div className="mt-1.5 grid grid-cols-2 gap-2">
                  <ConflictSide label="本地" side={conflict.local} />
                  <ConflictSide label="源站" side={conflict.remote} />
                </div>
              </li>
            ))}
          </ul>
          <div className="mt-4 flex justify-end gap-2">
            <Dialog.Close render={<Button variant="secondary">取消</Button>} />
            <Button
              variant="secondary"
              onClick={() => resolveConflicts("local")}
              disabled={syncMutation.isPending}
            >
              保留本地
            </Button>
            <Button
              onClick={() => resolveConflicts("remote")}
              disabled={syncMutation.isPending}
            >
              {syncMutation.isPending && (
                <CircleNotch className="mr-1 size-4 animate-spin" />
              )}
              保留源站
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root
        open={pendingDeletes !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPendingDeletes(null);
            setDeleteDismissed(true);
          }
        }}
      >
        <Dialog className="w-[min(92vw,28rem)] p-6">
          <Dialog.Title className="text-base font-semibold">
            从源站删除标签
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-kumo-subtle">
            源站有 {pendingDeletes?.length ?? 0} 个本地没有的标签，同步将把
            它们从源站 My Tags 删除：
          </Dialog.Description>
          <ul className="mt-3 max-h-48 space-y-1 overflow-y-auto text-xs">
            {pendingDeletes?.map((tag) => (
              <li key={tag} className="rounded bg-kumo-recessed px-2 py-1">
                <span className="font-mono">{tag}</span>
              </li>
            ))}
          </ul>
          <div className="mt-4 flex justify-end gap-2">
            <Dialog.Close render={<Button variant="secondary">取消</Button>} />
            <Button
              variant="destructive"
              onClick={confirmPendingDeletes}
              disabled={syncMutation.isPending}
            >
              {syncMutation.isPending && (
                <CircleNotch className="mr-1 size-4 animate-spin" />
              )}
              确认删除
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}
