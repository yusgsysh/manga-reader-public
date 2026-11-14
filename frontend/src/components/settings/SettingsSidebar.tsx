import { useState } from "react";
import { CaretDown } from "@phosphor-icons/react";
import { cn } from "@cloudflare/kumo";

interface SettingsNavItem {
  id: string;
  label: string;
}

interface SettingsNavGroup {
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
  // Desktop sidebar starts with every group expanded; groups can be collapsed
  // manually. (Mobile uses a separate two-level menu -> detail flow.)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  const toggle = (groupId: string) =>
    setCollapsed((prev) => ({ ...prev, [groupId]: !(prev[groupId] ?? false) }));

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
