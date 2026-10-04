import { Link, useLocation } from "react-router";
import {
  CaretDoubleLeft,
  CaretDoubleRight,
  Monitor,
  Moon,
  Sun,
} from "@phosphor-icons/react";
import { cn } from "@cloudflare/kumo";
import { useTheme } from "../../hooks/useTheme";
import type { ThemeMode } from "../../lib/theme";
import { IconButton, Logo } from "../ui";
import { PRIMARY_NAV, SECONDARY_NAV, isNavActive, type NavItem } from "./nav";
import { useNavigationContext } from "../../hooks/useNavigationContext";

const THEME_OPTIONS: { mode: ThemeMode; label: string; icon: typeof Sun }[] = [
  { mode: "light", label: "浅色", icon: Sun },
  { mode: "dark", label: "深色", icon: Moon },
  { mode: "system", label: "跟随系统", icon: Monitor },
];

function SidebarLink({
  item,
  active,
  collapsed,
}: {
  item: NavItem;
  active: boolean;
  collapsed: boolean;
}) {
  const Icon = item.icon;
  return (
    <Link
      to={item.to}
      aria-current={active ? "page" : undefined}
      title={collapsed ? item.label : undefined}
      className={cn(
        "flex items-center rounded-xl px-3 py-2 text-sm font-medium transition-[gap,background-color,color] duration-200 ease-out",
        collapsed ? "justify-center gap-0" : "gap-3",
        active
          ? "bg-[var(--app-accent)] text-[var(--app-accent-contrast)]"
          : "text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-default",
      )}
    >
      <Icon className="size-5 shrink-0" weight={active ? "fill" : "regular"} />
      {/*
        The label stays mounted and collapses via max-width/opacity so the text
        slides and fades with the rail instead of popping in at the end. Kept in
        the accessibility tree (no display:none) so collapsed links still have a
        readable name.
      */}
      <span
        className={cn(
          "min-w-0 overflow-hidden whitespace-nowrap transition-[max-width,opacity] duration-200 ease-out",
          collapsed ? "max-w-0 opacity-0" : "max-w-[10rem] opacity-100",
        )}
      >
        {item.label}
      </span>
    </Link>
  );
}

interface DesktopSidebarProps {
  collapsed: boolean;
  onToggle: () => void;
}

export function DesktopSidebar({ collapsed, onToggle }: DesktopSidebarProps) {
  const location = useLocation();
  const { mode, setMode } = useTheme();
  const { lastListRoute } = useNavigationContext();

  return (
    <aside className="fixed inset-y-0 left-0 z-40 hidden w-[var(--app-sidebar-width)] flex-col border-r border-kumo-hairline bg-kumo-base transition-[width] duration-200 ease-out lg:flex">
      <div className="relative flex h-14 shrink-0 items-center px-3">
        {/*
          The logo collapses its own width instead of being unmounted, so it
          slides/fades out with the rail. The toggle is pinned to the right edge
          and only nudges to the rail's center, avoiding a full jump when the
          header switches alignment.
        */}
        <Link
          to="/"
          aria-label="Manga Reader 首页"
          className={cn(
            "overflow-hidden whitespace-nowrap transition-[max-width,opacity] duration-200 ease-out",
            collapsed ? "max-w-0 opacity-0" : "max-w-[8rem] opacity-100",
          )}
        >
          <Logo />
        </Link>
        <IconButton
          label={collapsed ? "展开侧边栏" : "折叠侧边栏"}
          onClick={onToggle}
          className={cn(
            "absolute top-1/2 -translate-y-1/2 transition-[right] duration-200 ease-out",
            collapsed ? "right-[18px]" : "right-3",
          )}
        >
          {collapsed ? (
            <CaretDoubleRight className="size-5" weight="bold" />
          ) : (
            <CaretDoubleLeft className="size-5" weight="bold" />
          )}
        </IconButton>
      </div>

      <nav className="flex flex-1 flex-col gap-1 overflow-y-auto overflow-x-hidden px-3 py-2">
        {PRIMARY_NAV.map((item) => (
          <SidebarLink
            key={item.to}
            item={item}
            active={isNavActive(location.pathname, item.to, lastListRoute)}
            collapsed={collapsed}
          />
        ))}

        <div className="my-3 border-t border-kumo-hairline" />

        {SECONDARY_NAV.map((item) => (
          <SidebarLink
            key={item.to}
            item={item}
            active={isNavActive(location.pathname, item.to, lastListRoute)}
            collapsed={collapsed}
          />
        ))}
      </nav>

      <div className="shrink-0 border-t border-kumo-hairline p-3">
        <div
          className={cn(
            "flex items-center gap-1 rounded-xl bg-kumo-recessed p-1",
            collapsed && "flex-col",
          )}
        >
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
