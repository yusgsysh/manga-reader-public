import { useCallback, useEffect, useState, type ReactNode } from "react";
import {
  ArrowLeft,
  CornersIn,
  CornersOut,
  GearSix,
  Question,
  SquaresFour,
  X,
} from "@phosphor-icons/react";
import { cn } from "@cloudflare/kumo";
import type { ViewerPanel, ViewerSettings } from "@yui540/comimi-react";
import { useReaderPreferences } from "../../hooks/useReaderPreferences";
import { IconButton } from "../ui";
import { ComimiMascot } from "./ComimiMascot";

const AUTO_TURN_SECONDS = [3, 5, 8, 10, 15, 20, 30];

interface ReaderToolbarProps {
  title: string;
  fullscreen: boolean;
  activePanel: ViewerPanel;
  viewerSettings: ViewerSettings | null;
  onBack: () => void;
  onToggleFullscreen: () => void;
  onOpenPanel: (panel: ViewerPanel) => void;
  onClosePanel: () => void;
  onUpdateSettings: (patch: Partial<ViewerSettings>) => void;
}

function Segment({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "rounded-md px-2 py-1.5 text-xs font-medium transition-colors",
        active
          ? "bg-kumo-base text-kumo-default shadow-sm"
          : "text-kumo-subtle hover:text-kumo-default",
      )}
    >
      {children}
    </button>
  );
}

/**
 * Komga-style reader chrome: the way back and the title on the left, the
 * reader actions on the right. It replaces comimi's floating menu pill, so
 * the library's own top row is hidden in `index.css`.
 */
export function ReaderToolbar({
  title,
  fullscreen,
  activePanel,
  viewerSettings,
  onBack,
  onToggleFullscreen,
  onOpenPanel,
  onClosePanel,
  onUpdateSettings,
}: ReaderToolbarProps) {
  const [settingsOpen, setSettingsOpen] = useState(false);
  const { autoFullscreenOnStart, setAutoFullscreenOnStart } =
    useReaderPreferences();

  const closeSettings = useCallback(() => setSettingsOpen(false), []);

  const toggleSettings = useCallback(() => {
    setSettingsOpen((open) => {
      if (!open) onClosePanel();
      return !open;
    });
  }, [onClosePanel]);

  const openPanel = useCallback(
    (panel: ViewerPanel) => {
      setSettingsOpen(false);
      onOpenPanel(panel);
    },
    [onOpenPanel],
  );

  // Escape belongs to the open settings sheet; keep comimi's Esc-to-inline
  // shortcut from firing underneath it.
  useEffect(() => {
    if (!settingsOpen) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.stopPropagation();
      setSettingsOpen(false);
    };
    window.addEventListener("keydown", onKeyDown, true);
    return () => window.removeEventListener("keydown", onKeyDown, true);
  }, [settingsOpen]);

  const intervalSeconds = viewerSettings
    ? Math.round(viewerSettings.autoPageTurnIntervalMs / 1000)
    : 5;

  return (
    <>
      {settingsOpen && (
        <button
          type="button"
          aria-label="关闭阅读设置"
          className="fixed inset-0 z-[1000] cursor-default"
          onClick={closeSettings}
        />
      )}
      <header
        className={cn(
          "relative z-[1001] flex shrink-0 items-center gap-1 border-b border-kumo-hairline bg-kumo-base/95 px-2 backdrop-blur",
          "pt-[env(safe-area-inset-top)]",
          fullscreen && "fixed inset-x-0 top-0",
        )}
      >
      <IconButton label="返回画廊" onClick={onBack}>
        <ArrowLeft className="size-5" weight="bold" />
      </IconButton>

      <h1 className="min-w-0 flex-1 truncate px-1 text-[15px] font-semibold text-kumo-default">
        {title}
      </h1>

      <div className="flex items-center gap-0.5">
        <IconButton
          label={fullscreen ? "退出全屏" : "全屏"}
          aria-pressed={fullscreen}
          onClick={onToggleFullscreen}
        >
          {fullscreen ? (
            <CornersIn className="size-5" weight="bold" />
          ) : (
            <CornersOut className="size-5" weight="bold" />
          )}
        </IconButton>

        <IconButton
          label="键盘快捷键"
          aria-pressed={activePanel === "shortcuts"}
          className="hidden sm:flex"
          onClick={() => openPanel("shortcuts")}
        >
          <Question className="size-5" weight="bold" />
        </IconButton>

        <IconButton
          label="页面列表"
          aria-pressed={activePanel === "pages"}
          onClick={() => openPanel("pages")}
        >
          <SquaresFour className="size-5" weight="bold" />
        </IconButton>

        <IconButton
          label="阅读设置"
          aria-expanded={settingsOpen}
          onClick={toggleSettings}
        >
          <GearSix className="size-5" weight="bold" />
        </IconButton>

        <IconButton
          label="关于 comimi"
          aria-pressed={activePanel === "about"}
          onClick={() => openPanel("about")}
        >
          <ComimiMascot className="size-5 text-kumo-subtle" />
        </IconButton>
      </div>

      {settingsOpen && (
        <>
          <div
            role="dialog"
            aria-label="阅读设置"
            className="absolute top-full right-2 z-[1002] mt-1 w-[min(22rem,calc(100vw-1rem))] space-y-4 rounded-2xl border border-kumo-hairline bg-kumo-base p-4 shadow-xl"
          >
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-semibold text-kumo-default">阅读设置</h2>
              <IconButton label="关闭" size="sm" onClick={closeSettings}>
                <X className="size-4" weight="bold" />
              </IconButton>
            </div>

            <div>
              <div className="flex items-center justify-between gap-3">
                <span className="text-sm font-medium text-kumo-default">
                  开始阅读时自动全屏
                </span>
                <button
                  type="button"
                  role="switch"
                  aria-checked={autoFullscreenOnStart}
                  aria-label="开始阅读时自动全屏"
                  onClick={() =>
                    setAutoFullscreenOnStart(!autoFullscreenOnStart)
                  }
                  className={cn(
                    "relative h-6 w-11 shrink-0 rounded-full transition-colors",
                    autoFullscreenOnStart
                      ? "bg-[var(--app-accent)]"
                      : "bg-kumo-recessed",
                  )}
                >
                  <span
                    className={cn(
                      "absolute top-0.5 left-0.5 size-5 rounded-full bg-white shadow transition-transform",
                      autoFullscreenOnStart && "translate-x-5",
                    )}
                  />
                </button>
              </div>
              <p className="mt-1 text-xs leading-relaxed text-kumo-subtle">
                从画廊点「开始阅读」时直接进入全屏；浏览器不允许时至少占满窗口。
              </p>
            </div>

            <div>
              <div className="mb-1.5 text-xs font-medium text-kumo-subtle">
                阅读方向
              </div>
              <div className="grid grid-cols-2 gap-1 rounded-lg bg-kumo-recessed p-1">
                <Segment
                  active={viewerSettings?.readingDirection === "rtl"}
                  onClick={() =>
                    onUpdateSettings({ readingDirection: "rtl" })
                  }
                >
                  从右到左
                </Segment>
                <Segment
                  active={viewerSettings?.readingDirection === "ltr"}
                  onClick={() =>
                    onUpdateSettings({ readingDirection: "ltr" })
                  }
                >
                  从左到右
                </Segment>
              </div>
            </div>

            <div>
              <div className="mb-1.5 text-xs font-medium text-kumo-subtle">
                翻页模式
              </div>
              <div className="grid grid-cols-2 gap-1 rounded-lg bg-kumo-recessed p-1">
                <Segment
                  active={viewerSettings?.pageTurnMode === "single"}
                  onClick={() => onUpdateSettings({ pageTurnMode: "single" })}
                >
                  单页
                </Segment>
                <Segment
                  active={viewerSettings?.pageTurnMode === "spread"}
                  onClick={() => onUpdateSettings({ pageTurnMode: "spread" })}
                >
                  双页
                </Segment>
              </div>
            </div>

            <div className="flex items-center justify-between gap-3">
              <span className="text-sm font-medium text-kumo-default">
                显示封面页
              </span>
              <button
                type="button"
                role="switch"
                aria-checked={viewerSettings?.hasCover ?? false}
                aria-label="显示封面页"
                onClick={() =>
                  onUpdateSettings({ hasCover: !viewerSettings?.hasCover })
                }
                disabled={!viewerSettings}
                className={cn(
                  "relative h-6 w-11 shrink-0 rounded-full transition-colors disabled:opacity-50",
                  viewerSettings?.hasCover
                    ? "bg-[var(--app-accent)]"
                    : "bg-kumo-recessed",
                )}
              >
                <span
                  className={cn(
                    "absolute top-0.5 left-0.5 size-5 rounded-full bg-white shadow transition-transform",
                    viewerSettings?.hasCover && "translate-x-5",
                  )}
                />
              </button>
            </div>

            <div className="flex items-center justify-between gap-3">
              <span className="text-sm font-medium text-kumo-default">
                自动翻页间隔
              </span>
              <select
                aria-label="自动翻页间隔"
                value={intervalSeconds}
                onChange={(event) =>
                  onUpdateSettings({
                    autoPageTurnIntervalMs: Number(event.target.value) * 1000,
                  })
                }
                disabled={!viewerSettings}
                className="rounded-lg border border-kumo-hairline bg-kumo-base px-2 py-1 text-sm text-kumo-default disabled:opacity-50"
              >
                {AUTO_TURN_SECONDS.map((seconds) => (
                  <option key={seconds} value={seconds}>
                    {seconds} 秒
                  </option>
                ))}
              </select>
            </div>

            <p className="border-t border-kumo-hairline pt-3 text-xs text-kumo-subtle">
              键盘快捷键见右上角的 <kbd className="rounded bg-kumo-recessed px-1">?</kbd> 按钮。
            </p>
          </div>
        </>
      )}
    </header>
    </>
  );
}
