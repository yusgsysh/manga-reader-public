import { useState } from "react";
import { Button, DatePicker, Popover } from "@cloudflare/kumo";
import { CalendarBlank, X } from "@phosphor-icons/react";
import { parseJumpSeekInput } from "../../lib/jumpSeek";
import type { ListingNav, ListingNavOptions } from "../../types/gallery";

const QUICK_JUMPS = [
  { value: "1d", label: "1天" },
  { value: "3d", label: "3天" },
  { value: "1w", label: "1周" },
  { value: "1m", label: "1月" },
  { value: "1y", label: "1年" },
] as const;

interface JumpSeekPanelProps {
  nav?: ListingNav;
  value?: ListingNavOptions;
  onChange: (value: ListingNavOptions) => void;
  disabled?: boolean;
  /**
   * How the date calendar is presented.
   * - `"popover"` — behind a button (used inside an inline panel).
   * - `"inline"` — rendered directly (used inside an outer popover, to avoid
   *   nesting one popover inside another).
   */
  calendar?: "popover" | "inline";
  /** Called after a change is applied; lets an outer popover dismiss itself. */
  onCommit?: () => void;
}

function parseSeekDate(value?: string): Date | undefined {
  if (!value) return undefined;
  const match = /^(\d{2,4})-(\d{1,2})(?:-(\d{1,2}))?$/.exec(value);
  if (!match) return undefined;
  let year = Number(match[1]);
  if (year < 100) year += 2000;
  const month = Number(match[2]);
  const day = match[3] ? Number(match[3]) : 1;
  if (month < 1 || month > 12 || day < 1 || day > 31) return undefined;
  return new Date(year, month - 1, day);
}

function formatSeekDate(date: Date): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

export function JumpSeekPanel({
  nav,
  value,
  onChange,
  disabled,
  calendar = "popover",
  onCommit,
}: JumpSeekPanelProps) {
  const [open, setOpen] = useState(false);
  const active = Boolean(value?.seek || value?.jump);
  const hasRange = Boolean(nav?.min_date && nav?.max_date);

  const handleQuickJump = (jump: string) => {
    const result = parseJumpSeekInput(jump);
    if (!result || result.kind !== "jump") return;
    onChange({ jump: result.value });
    onCommit?.();
  };

  const handleSelectDate = (date?: Date) => {
    if (!date) return;
    onChange({ seek: formatSeekDate(date) });
    setOpen(false);
    onCommit?.();
  };

  const handleClear = () => {
    onChange({});
    setOpen(false);
    onCommit?.();
  };

  const datePicker = (
    <DatePicker
      mode="single"
      selected={parseSeekDate(value?.seek)}
      onChange={handleSelectDate}
    />
  );

  return (
    <div className="space-y-4">
      <h2 className="text-sm font-medium">浏览定位</h2>

      {calendar === "inline" ? (
        datePicker
      ) : (
        <Popover open={open} onOpenChange={setOpen}>
          <Popover.Trigger
            disabled={disabled}
            render={
              <Button
                variant="secondary"
                size="sm"
                disabled={disabled}
                className="w-full justify-start"
              />
            }
          >
            <CalendarBlank className="size-4 shrink-0" weight="bold" />
            <span className="min-w-0 truncate">{value?.seek ?? "选择日期"}</span>
          </Popover.Trigger>
          <Popover.Content side="bottom" align="start" className="p-2">
            {datePicker}
          </Popover.Content>
        </Popover>
      )}

      <div className="space-y-2">
        <span className="text-xs text-kumo-subtle">快捷</span>
        <div className="flex flex-wrap gap-1">
          {QUICK_JUMPS.map((jump) => {
            const selected = value?.jump === jump.value;
            return (
              <Button
                key={jump.value}
                size="sm"
                variant={selected ? "secondary" : "ghost"}
                aria-pressed={selected}
                disabled={disabled}
                onClick={() => handleQuickJump(jump.value)}
                className={
                  selected
                    ? "text-[var(--app-accent)] ring-[var(--app-accent)]"
                    : undefined
                }
              >
                {jump.label}
              </Button>
            );
          })}
        </div>
      </div>

      {hasRange && (
        <div className="space-y-1 text-xs text-kumo-subtle">
          <span className="block">可定位范围</span>
          <div className="flex flex-col gap-0.5">
            <span>{nav?.min_date}</span>
            <span>{nav?.max_date}</span>
          </div>
        </div>
      )}

      {active && (
        <Button
          variant="ghost"
          size="sm"
          onClick={handleClear}
          disabled={disabled}
          className="text-kumo-subtle"
        >
          <X className="size-3.5" weight="bold" />
          清除
        </Button>
      )}
    </div>
  );
}
