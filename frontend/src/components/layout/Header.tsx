import { Link, useLocation, useNavigate } from "react-router";
import { useState } from "react";
import { DropdownMenu, cn } from "@cloudflare/kumo";
import {
  CaretDown,
  List,
  MagnifyingGlass,
  Monitor,
  Moon,
  Sun,
} from "@phosphor-icons/react";
import { useTheme } from "../../hooks/useTheme";
import type { ThemeMode } from "../../lib/theme";
import { IconButton, Logo } from "../ui";
import { PRIMARY_NAV, SECONDARY_NAV, isNavActive } from "./nav";
import { MobileDrawer } from "./MobileDrawer";

const THEME_OPTIONS: { mode: ThemeMode; label: string; icon: typeof Sun }[] = [
  { mode: "light", label: "浅色", icon: Sun },
  { mode: "dark", label: "深色", icon: Moon },
  { mode: "system", label: "跟随系统", icon: Monitor },
];

function ThemeIcon({ mode }: { mode: ThemeMode }) {
  const Icon = THEME_OPTIONS.find((o) => o.mode === mode)?.icon ?? Monitor;
  return <Icon className="size-5" weight="fill" />;
}

export function Header() {
  const location = useLocation();
  const navigate = useNavigate();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const { mode, setMode } = useTheme();

  return (
    <>
      <header className="sticky top-0 z-50 border-b border-kumo-hairline bg-kumo-base/80 pt-[env(safe-area-inset-top)] backdrop-blur-md">
        <div className="app-container flex h-14 items-center gap-2">
          <IconButton
            label="打开菜单"
            className="md:hidden"
            onClick={() => setDrawerOpen(true)}
          >
            <List className="size-5" weight="bold" />
          </IconButton>

          <Link
            to="/"
            className="flex flex-1 items-center justify-center md:flex-none md:justify-start"
            aria-label="Manga Reader 首页"
          >
            <Logo />
          </Link>

          {/* Desktop primary nav */}
          <nav className="ml-3 hidden items-center gap-0.5 md:flex">
            {PRIMARY_NAV.map((item) => {
              const active = isNavActive(location.pathname, item.to);
              return (
                <Link
                  key={item.to}
                  to={item.to}
                  aria-current={active ? "page" : undefined}
                  className={cn(
                    "rounded-lg px-3 py-1.5 text-sm font-medium transition-colors",
                    active
                      ? "bg-kumo-contrast text-kumo-inverse"
                      : "text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-default",
                  )}
                >
                  {item.label}
                </Link>
              );
            })}
          </nav>

          <div className="hidden flex-1 md:block" />

          {/* Desktop actions */}
          <div className="hidden items-center gap-1 md:flex">
            <DropdownMenu>
              <DropdownMenu.Trigger
                render={
                  <button
                    type="button"
                    className="flex items-center gap-1 rounded-lg px-3 py-1.5 text-sm font-medium text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default"
                  >
                    更多
                    <CaretDown className="size-3.5" weight="bold" />
                  </button>
                }
              />
              <DropdownMenu.Content align="end">
                {SECONDARY_NAV.map((item) => (
                  <DropdownMenu.LinkItem
                    key={item.to}
                    href={item.to}
                    icon={item.icon}
                  >
                    {item.label}
                  </DropdownMenu.LinkItem>
                ))}
              </DropdownMenu.Content>
            </DropdownMenu>

            <DropdownMenu>
              <DropdownMenu.Trigger
                render={
                  <IconButton label="切换主题" aria-haspopup="menu">
                    <ThemeIcon mode={mode} />
                  </IconButton>
                }
              />
              <DropdownMenu.Content align="end">
                {THEME_OPTIONS.map((option) => (
                  <DropdownMenu.Item
                    key={option.mode}
                    icon={option.icon}
                    selected={mode === option.mode}
                    onClick={() => setMode(option.mode)}
                  >
                    {option.label}
                  </DropdownMenu.Item>
                ))}
              </DropdownMenu.Content>
            </DropdownMenu>
          </div>

          {/* Mobile search */}
          <IconButton
            label="搜索"
            className="md:hidden"
            onClick={() => navigate("/search")}
          >
            <MagnifyingGlass className="size-5" weight="bold" />
          </IconButton>
        </div>
      </header>

      <MobileDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} />
    </>
  );
}
