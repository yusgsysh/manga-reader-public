import type { Icon } from "@phosphor-icons/react";
import {
  BookmarkSimple,
  Books,
  ClockCounterClockwise,
  DownloadSimple,
  Fire,
  Gear,
  House,
  Images,
  MagnifyingGlass,
} from "@phosphor-icons/react";

export interface NavItem {
  to: string;
  label: string;
  icon: Icon;
}

export const PRIMARY_NAV: NavItem[] = [
  { to: "/", label: "首页", icon: House },
  { to: "/watched", label: "订阅", icon: BookmarkSimple },
  { to: "/popular", label: "热门", icon: Fire },
  { to: "/search", label: "搜索", icon: MagnifyingGlass },
  { to: "/bookshelf", label: "书架", icon: Books },
  { to: "/gallery", label: "画廊", icon: Images },
];

export const SECONDARY_NAV: NavItem[] = [
  { to: "/recently-read", label: "阅读历史", icon: ClockCounterClockwise },
  { to: "/downloads", label: "下载管理", icon: DownloadSimple },
  { to: "/settings", label: "设置", icon: Gear },
];

export function isNavActive(pathname: string, to: string, lastListRoute?: string): boolean {
  if (pathname.startsWith("/gallery/")) {
    return to === (lastListRoute || "/");
  }
  return to === "/" ? pathname === "/" : pathname.startsWith(to);
}
