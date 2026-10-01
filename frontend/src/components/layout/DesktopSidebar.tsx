import { Link, useLocation } from "react-router";
import { Monitor, Moon, Sun } from "@phosphor-icons/react";
import { cn } from "@cloudflare/kumo";
import { useTheme } from "../../hooks/useTheme";
import type { ThemeMode } from "../../lib/theme";
import { Logo } from "../ui";
import { PRIMARY_NAV, SECONDARY_NAV, isNavActive, type NavItem } from "./nav";

const THEME_OPTIONS: { mode: ThemeMode; label: string; icon: typeof Sun }[] = [
  { mode: "light", label: "浅色", icon: Sun },
  { mode: "dark", label: "深色", icon: Moon },
  { mode: "system", label: "跟随系统", icon: Monitor },
];

function SidebarLink({ item, active }: { item: NavItem; active: boolean }) {
  const Icon = item.icon;
  return (
    <Link
      to={item.to}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex items-center gap-3 rounded-xl px-3 py-2 text-sm font-medium transition-colors",
        active
          ? "bg-[var(--app-accent)] text-[var(--app-accent-contrast)]"
          : "text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-default",
      )}
    >
      <Icon className="size-5 shrink-0" weight={active ? "fill" : "regular"} />
      {item.label}
    </Link>
  );
}

export function DesktopSidebar() {
  const location = useLocation();
  const { mode, setMode } = useTheme();

  return (
    <aside className="fixed inset-y-0 left-0 z-40 hidden w-[var(--app-sidebar-width)] flex-col border-r border-kumo-hairline bg-kumo-base lg:flex">
      <div className="flex h-14 shrink-0 items-center px-4">
        <Link to="/" aria-label="Manga Reader 首页">
          <Logo />
        </Link>
      </div>

      <nav className="flex flex-1 flex-col gap-1 overflow-y-auto px-3 py-2">
        {PRIMARY_NAV.map((item) => (
          <SidebarLink
            key={item.to}
            item={item}
            active={isNavActive(location.pathname, item.to)}
          />
        ))}

        <div className="my-3 border-t border-kumo-hairline" />

        {SECONDARY_NAV.map((item) => (
          <SidebarLink
            key={item.to}
            item={item}
            active={isNavActive(location.pathname, item.to)}
          />
        ))}
      </nav>

      <div className="shrink-0 border-t border-kumo-hairline p-3">
        <div className="flex items-center gap-1 rounded-xl bg-kumo-recessed p-1">
          {THEME_OPTIONS.map((option) => {
            const Icon = option.icon;
            const active = mode === option.mode;
            return (
              <button
                key={option.mode}
                type="button"
                onClick={() => setMode(option.mode)}
                aria-label={option.label}
                aria-pressed={active}
                title={option.label}
                className={cn(
                  "flex flex-1 items-center justify-center rounded-lg py-1.5 transition-colors",
                  active
                    ? "bg-[var(--app-accent-soft)] text-[var(--app-accent)]"
                    : "text-kumo-subtle hover:text-kumo-default",
                )}
              >
                <Icon className="size-4" weight={active ? "fill" : "regular"} />
              </button>
            );
          })}
        </div>
      </div>
    </aside>
  );
}
