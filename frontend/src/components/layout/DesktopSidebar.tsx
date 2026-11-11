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
      <span className="truncate">{item.label}</span>
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
    <>
      {/*
        Collapsing sets the rail width to 0, so the sidebar disappears entirely
        rather than shrinking into an icon rail. `overflow-hidden` clips the
        children while the width animates, and `inert` keeps the hidden links
        out of focus and the accessibility tree.
      */}
      <aside
        inert={collapsed}
        className={cn(
          "fixed inset-y-0 left-0 z-40 hidden w-[var(--app-sidebar-width)] flex-col overflow-hidden bg-kumo-base transition-[width] duration-200 ease-out lg:flex",
          collapsed
            ? "border-r border-transparent"
            : "border-r border-kumo-hairline",
        )}
      >
        {/*
          The contents fade while the rail narrows, so the collapse reads as the
          panel receding rather than icons and labels being squeezed out.
        */}
        <div
          className={cn(
            "flex h-full flex-col transition-opacity duration-150 ease-out",
            collapsed && "opacity-0",
          )}
        >
          <div className="relative flex h-14 shrink-0 items-center px-3">
            <Link to="/" aria-label="Manga Reader 首页">
              <Logo />
            </Link>
            {!collapsed && (
              <IconButton
                label="折叠侧边栏"
                onClick={onToggle}
                className="absolute top-1/2 right-3 -translate-y-1/2"
              >
                <CaretDoubleLeft className="size-5" weight="bold" />
              </IconButton>
            )}
          </div>

          <nav className="flex flex-1 flex-col gap-1 overflow-y-auto overflow-x-hidden px-3 py-2">
            {PRIMARY_NAV.map((item) => (
              <SidebarLink
                key={item.to}
                item={item}
                active={isNavActive(location.pathname, item.to, lastListRoute)}
              />
            ))}

            <div className="my-3 border-t border-kumo-hairline" />

            {SECONDARY_NAV.map((item) => (
              <SidebarLink
                key={item.to}
                item={item}
                active={isNavActive(location.pathname, item.to, lastListRoute)}
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
                    <Icon
                      className="size-4"
                      weight={active ? "fill" : "regular"}
                    />
                  </button>
                );
              })}
            </div>
          </div>
        </div>
      </aside>

      {/*
        With the rail gone, the in-rail toggle is gone too. The expand button
        takes over its top-left corner, so the control returns to where the
        rail's own toggle sits: collapsing and expanding feel like the same
        corner. `--app-content-inset` reserves the gutter it floats in.
      */}
      {collapsed && (
        <IconButton
          label="展开侧边栏"
          onClick={onToggle}
          className="fixed top-3 left-3 z-30 hidden bg-kumo-recessed shadow-sm hover:bg-kumo-tint lg:flex"
        >
          <CaretDoubleRight className="size-5" weight="bold" />
        </IconButton>
      )}
    </>
  );
}
