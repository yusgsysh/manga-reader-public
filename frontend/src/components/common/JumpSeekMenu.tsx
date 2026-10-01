import { useState } from "react";
import { Popover } from "@cloudflare/kumo";
import { Crosshair } from "@phosphor-icons/react";
import { IconButton } from "../ui";
import { JumpSeekPanel } from "./JumpSeekPanel";
import type { ListingNav, ListingNavOptions } from "../../types/gallery";

interface JumpSeekMenuProps {
  nav?: ListingNav;
  value?: ListingNavOptions;
  onChange: (value: ListingNavOptions) => void;
  disabled?: boolean;
}

function activeLabel(value?: ListingNavOptions): string {
  if (value?.seek) return `浏览定位：${value.seek}`;
  if (value?.jump) return `浏览定位：${value.jump}`;
  return "浏览定位";
}

export function JumpSeekMenu({
  nav,
  value,
  onChange,
  disabled,
}: JumpSeekMenuProps) {
  const [open, setOpen] = useState(false);
  const active = Boolean(value?.seek || value?.jump);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Popover.Trigger
        disabled={disabled}
        render={
          <IconButton
            label={activeLabel(value)}
            size="sm"
            disabled={disabled}
            className={
              active
                ? "bg-[var(--app-accent-soft)] text-[var(--app-accent)] hover:bg-[var(--app-accent-soft)] hover:text-[var(--app-accent)]"
                : undefined
            }
          >
            <Crosshair className="size-4" weight="bold" />
          </IconButton>
        }
      />
      <Popover.Content side="bottom" align="end" sideOffset={8} className="p-4">
        <JumpSeekPanel
          nav={nav}
          value={value}
          onChange={onChange}
          disabled={disabled}
          calendar="inline"
          onCommit={() => setOpen(false)}
        />
      </Popover.Content>
    </Popover>
  );
}
