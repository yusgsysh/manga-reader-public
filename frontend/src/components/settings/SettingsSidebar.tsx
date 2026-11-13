import { useState } from "react";
import { CaretDown } from "@phosphor-icons/react";
import { cn } from "@cloudflare/kumo";

export interface SettingsNavItem {
  id: string;
  label: string;
}

export interface SettingsNavGroup {
  id: string;
  label: string;
  items: SettingsNavItem[];
}

export function SettingsSidebar({
  groups,
  activeId,
  onNavigate,
  className,
}: {
  groups: SettingsNavGroup[];
  activeId: string;
  onNavigate: (id: string) => void;
  className?: string;
}) {
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  // Adjust state during render (same pattern as SyncSection): the group
  // containing the active section is always expanded, so scrolling can never
  // leave the highlighted item hidden behind a collapsed group.
  const activeGroupId = groups.find((group) =>
    group.items.some((item) => item.id === activeId),
  )?.id;
  if (activeGroupId && collapsed[activeGroupId]) {
    setCollapsed((prev) => ({ ...prev, [activeGroupId]: false }));
  }

  const toggle = (groupId: string) =>
    setCollapsed((prev) => ({ ...prev, [groupId]: !prev[groupId] }));

  return (
    <nav className={cn("space-y-1", className)} aria-label="设置导航">
      {groups.map((group) => {
        const isCollapsed = collapsed[group.id] ?? false;
        const isActiveGroup = group.items.some((item) => item.id === activeId);
        return (
          <div key={group.id}>
            <button
              type="button"
              onClick={() => toggle(group.id)}
              aria-expanded={!isCollapsed}
              className={cn(
                "flex w-full items-center justify-between rounded-lg px-2 py-1.5 text-xs font-medium transition-colors",
                isActiveGroup
                  ? "text-kumo-default"
                  : "text-kumo-subtle hover:text-kumo-default",
              )}
            >
              <span>{group.label}</span>
              <CaretDown
                className={cn(
                  "size-3.5 text-kumo-subtle transition-transform",
                  isCollapsed && "-rotate-90",
                )}
              />
            </button>
            {!isCollapsed && (
              <ul className="mt-0.5 space-y-0.5">
                {group.items.map((item) => {
                  const active = item.id === activeId;
                  return (
                    <li key={item.id}>
                      <button
                        type="button"
                        onClick={() => onNavigate(item.id)}
                        aria-current={active ? "true" : undefined}
                        className={cn(
                          "w-full rounded-lg px-2 py-1.5 pl-4 text-left text-sm transition-colors",
                          active
                            ? "bg-[var(--app-accent-soft)] font-medium text-[var(--app-accent)]"
                            : "text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-default",
                        )}
                      >
                        {item.label}
                      </button>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        );
      })}
    </nav>
  );
}
