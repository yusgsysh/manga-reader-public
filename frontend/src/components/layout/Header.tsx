import { Link, useLocation } from "react-router";
import { useState } from "react";
import { Button, DropdownMenu, useKumoToastManager } from "@cloudflare/kumo";
import { Loader2, Menu, Moon, RefreshCw, Sun, Monitor, X } from "lucide-react";
import { useTheme } from "../../hooks/useTheme";
import { useTagTranslation } from "../../hooks/useTagTranslation";
import type { ThemeMode } from "../../lib/theme";

const NAV_ITEMS = [
  { to: "/", label: "首页" },
  { to: "/watched", label: "订阅" },
  { to: "/popular", label: "热门" },
  { to: "/search", label: "搜索" },
  { to: "/bookshelf", label: "书架" },
  { to: "/recently-read", label: "最近阅读" },
];

const THEME_OPTIONS: { mode: ThemeMode; label: string; icon: typeof Sun }[] = [
  { mode: "system", label: "跟随系统", icon: Monitor },
  { mode: "light", label: "浅色", icon: Sun },
  { mode: "dark", label: "深色", icon: Moon },
];

export function Header() {
  const location = useLocation();
  const [mobileOpen, setMobileOpen] = useState(false);
  const { mode, setMode } = useTheme();

  return (
    <header className="sticky top-0 z-50 border-b border-kumo-hairline bg-kumo-elevated/80 backdrop-blur-sm">
      <div className="mx-auto flex h-14 max-w-7xl items-center justify-between px-4">
        <Link to="/" className="text-lg font-bold">
          Manga Reader
        </Link>

        {/* Desktop nav */}
        <nav className="hidden items-center gap-1 md:flex">
          {NAV_ITEMS.map((item) => {
            const isActive =
              item.to === "/"
                ? location.pathname === "/"
                : location.pathname.startsWith(item.to);
            return (
              <Link key={item.to} to={item.to}>
                <Button
                  variant={isActive ? "primary" : "ghost"}
                  size="sm"
                  className="text-sm"
                >
                  {item.label}
                </Button>
              </Link>
            );
          })}
        </nav>

        <div className="flex items-center gap-1">
          {/* Tag translation database update */}
          <TagDbUpdateButton />

          {/* Theme toggle */}
          <DropdownMenu>
            <DropdownMenu.Trigger
              render={
                <Button
                  variant="ghost"
                  size="sm"
                  aria-label="切换主题"
                >
                  <ThemeIcon mode={mode} />
                </Button>
              }
            />
            <DropdownMenu.Content align="end">
              {THEME_OPTIONS.map((option) => (
                <DropdownMenu.Item
                  key={option.mode}
                  icon={<option.icon className="size-4" />}
                  selected={mode === option.mode}
                  onClick={() => setMode(option.mode)}
                >
                  {option.label}
                </DropdownMenu.Item>
              ))}
            </DropdownMenu.Content>
          </DropdownMenu>

          {/* Mobile menu button */}
          <Button
            variant="ghost"
            size="sm"
            className="md:hidden"
            onClick={() => setMobileOpen(!mobileOpen)}
            aria-label={mobileOpen ? "关闭菜单" : "打开菜单"}
          >
            {mobileOpen ? (
              <X className="size-5" />
            ) : (
              <Menu className="size-5" />
            )}
          </Button>
        </div>
      </div>

      {/* Mobile nav */}
      {mobileOpen && (
        <nav className="border-t border-kumo-hairline bg-kumo-elevated px-4 pb-4 pt-2 md:hidden">
          {NAV_ITEMS.map((item) => {
            const isActive =
              item.to === "/"
                ? location.pathname === "/"
                : location.pathname.startsWith(item.to);
            return (
              <Link
                key={item.to}
                to={item.to}
                onClick={() => setMobileOpen(false)}
              >
                <Button
                  variant={isActive ? "primary" : "ghost"}
                  size="sm"
                  className="mb-1 w-full justify-start text-sm"
                >
                  {item.label}
                </Button>
              </Link>
            );
          })}
        </nav>
      )}
    </header>
  );
}

function ThemeIcon({ mode }: { mode: ThemeMode }) {
  const Icon = THEME_OPTIONS.find((o) => o.mode === mode)?.icon ?? Monitor;
  return <Icon className="size-4" />;
}

function TagDbUpdateButton() {
  const { info, updateStatus, update } = useTagTranslation();
  const toast = useKumoToastManager();
  const updating = updateStatus === "loading";

  const handleUpdate = async () => {
    if (updating) return;
    try {
      const result = await update();
      toast.add({
        title: result.changed
          ? "标签翻译数据库已更新"
          : "翻译数据库已是最新",
        description: info?.sha ? `版本 ${info.sha.slice(0, 7)}` : undefined,
        variant: result.changed ? "success" : "info",
      });
    } catch (error) {
      toast.add({
        title: "更新标签翻译数据库失败",
        description: error instanceof Error ? error.message : undefined,
        variant: "error",
      });
    }
  };

  const versionText = info?.sha
    ? `翻译数据库 ${info.version ?? ""} · ${info.sha.slice(0, 7)}\n更新于 ${new Date(
        info.loadedAt,
      ).toLocaleString()}`
    : "标签翻译数据库未加载";

  return (
    <Button
      variant="ghost"
      size="sm"
      onClick={handleUpdate}
      disabled={updating}
      aria-label="更新标签翻译数据库"
      title={versionText}
    >
      {updating ? (
        <Loader2 className="size-4 animate-spin" />
      ) : (
        <RefreshCw className="size-4" />
      )}
    </Button>
  );
}
