import { useState } from "react";
import { Button, Input } from "@cloudflare/kumo";
import { parseJumpSeekInput } from "../../lib/jumpSeek";
import type { ListingNav, ListingNavOptions } from "../../types/gallery";

const QUICK_JUMPS = ["1d", "3d", "1w", "1m", "1y"] as const;

interface JumpSeekBarProps {
  nav?: ListingNav;
  value?: ListingNavOptions;
  onChange: (value: ListingNavOptions) => void;
  disabled?: boolean;
}

export function JumpSeekBar({
  nav,
  value,
  onChange,
  disabled,
}: JumpSeekBarProps) {
  const [input, setInput] = useState("");
  const parsed = parseJumpSeekInput(input);
  const invalid = input.trim() !== "" && parsed === null;
  const active = value?.seek || value?.jump;

  const apply = (inputValue: string) => {
    const result = parseJumpSeekInput(inputValue);
    if (!result) return;
    onChange(
      result.kind === "seek" ? { seek: result.value } : { jump: result.value },
    );
  };

  const handleApply = () => apply(input);

  const handleQuickJump = (jump: string) => {
    setInput(jump);
    onChange({ jump });
  };

  const handleClear = () => {
    setInput("");
    onChange({});
  };

  const range =
    nav?.min_date && nav?.max_date ? `${nav.min_date} ~ ${nav.max_date}` : "";

  return (
    <div className="card-surface space-y-3 p-4">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm font-medium">跳转/定位</span>
        <div className="flex min-w-[220px] flex-1 gap-2">
          <Input
            placeholder="日期或偏移，如 2020 / 1w"
            aria-label="跳转或定位"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") handleApply();
            }}
            disabled={disabled}
            className="flex-1"
          />
          <Button onClick={handleApply} disabled={disabled || !parsed}>
            跳转
          </Button>
          {active && (
            <Button
              variant="secondary"
              onClick={handleClear}
              disabled={disabled}
              aria-label="清除跳转/定位"
            >
              清除
            </Button>
          )}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs text-kumo-subtle">快捷：</span>
        {QUICK_JUMPS.map((jump) => (
          <button
            key={jump}
            type="button"
            onClick={() => handleQuickJump(jump)}
            disabled={disabled}
            className="rounded-full border border-kumo-hairline px-2.5 py-1 text-xs font-medium text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default disabled:opacity-50"
          >
            {jump}
          </button>
        ))}
        {active && (
          <span className="inline-flex items-center rounded-full bg-[var(--app-chip-bg)] px-2.5 py-1 text-xs font-medium text-[var(--app-chip-fg)]">
            {value?.seek ? `定位 ${value.seek}` : `偏移 ${value?.jump}`}
          </span>
        )}
        {range && (
          <span className="ml-auto text-xs text-kumo-subtle">
            可定位日期：{range}
          </span>
        )}
      </div>

      {invalid && (
        <p className="text-xs text-[var(--app-danger,#dc2626)]">
          格式无效：请输入年份/日期（如 2020、2020-01）或偏移（如 3d、1w、1m、1y）。
        </p>
      )}
    </div>
  );
}
