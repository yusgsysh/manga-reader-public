import { useState } from "react";
import { Link, useLocation, useNavigate } from "react-router";
import { List, MagnifyingGlass, SignOut } from "@phosphor-icons/react";
import { cn } from "@cloudflare/kumo";
import { IconButton, Logo } from "../ui";
import { MobileDrawer } from "./MobileDrawer";
import {
  PRIMARY_NAV,
  SECONDARY_NAV,
  isNavActive,
  type NavItem,
} from "./nav";
import { useNavigationContext } from "../../hooks/useNavigationContext";
import { useAuth } from "../../context/auth";

function TopNavLink({
  item,
  active,
  iconOnly = false,
}: {
  item: NavItem;
  active: boolean;
  iconOnly?: boolean;
}) {
  const Icon = item.icon;
  return (
    <Link
      to={item.to}
      aria-current={active ? "page" : undefined}
      aria-label={iconOnly ? item.label : undefined}
      title={iconOnly ? item.label : undefined}
      className={cn(
        "flex items-center rounded-lg transition-colors",
        iconOnly
          ? "size-9 justify-center"
          : "gap-2 px-3 py-1.5 text-sm font-medium whitespace-nowrap",
        active
          ? "bg-[var(--app-accent-soft)] text-[var(--app-accent)]"
          : "text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-default",
      )}
    >
      <Icon
        className={iconOnly ? "size-5" : "size-4"}
        weight={active ? "fill" : "regular"}
      />
      {!iconOnly && <span>{item.label}</span>}
    </Link>
  );
}

export function Header() {
  const navigate = useNavigate();
  const location = useLocation();
  const { lastListRoute } = useNavigationContext();
  const {
    logout,
    isLoggingOut,
  } = useAuth();
  const [drawerOpen, setDrawerOpen] = useState(false);

  const isActive = (to: string) =>
    isNavActive(location.pathname, to, lastListRoute);

  return (
    <>
      {/*
        One sticky bar for every viewport: brand, primary destinations, and
        utilities. Below lg the destinations move into the drawer and the bar
        falls back to the compact menu / logo / search layout.
      */}
      <header className="sticky top-0 z-50 border-b border-kumo-hairline bg-kumo-base/95 pt-[env(safe-area-inset-top)]">
        <div className="app-container flex h-14 items-center gap-2">
          <IconButton
            label="打开菜单"
            onClick={() => setDrawerOpen(true)}
            className="lg:hidden"
          >
            <List className="size-5" weight="bold" />
          </IconButton>

          <nav
            aria-label="主导航"
            className="hidden min-w-0 flex-1 items-center gap-1 lg:flex"
          >
            {PRIMARY_NAV.map((item) => (
              <TopNavLink key={item.to} item={item} active={isActive(item.to)} />
            ))}
          </nav>

          <Link
            to="/"
            aria-label="Manga Reader 首页"
            className="flex flex-1 items-center justify-center lg:hidden"
          >
            <Logo />
          </Link>

          <div className="hidden items-center gap-1 lg:flex">
            <nav aria-label="辅助导航" className="flex items-center gap-1">
              {SECONDARY_NAV.map((item) => (
                <TopNavLink
                  key={item.to}
                  item={item}
                  active={isActive(item.to)}
                  iconOnly
                />
              ))}
            </nav>

            <IconButton
              label="退出登录"
              onClick={() => void logout().catch(() => undefined)}
              disabled={isLoggingOut}
            >
              <SignOut className="size-5" weight="bold" />
            </IconButton>
          </div>

          <IconButton
            label="搜索"
            onClick={() => navigate("/search")}
            className="lg:hidden"
          >
            <MagnifyingGlass className="size-5" weight="bold" />
          </IconButton>
        </div>
      </header>

      <MobileDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} />
    </>
  );
}
