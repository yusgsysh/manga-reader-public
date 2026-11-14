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
import type { ViewerSettings } from "@yui540/comimi-react";
import { IconButton } from "../ui";
import { ComimiMascot } from "./ComimiMascot";
import {
  ReaderAbout,
  ReaderPageList,
  ReaderShortcuts,
} from "./ReaderToolbarPanels";

type ToolbarPanel = "settings" | "shortcuts" | "pages" | "about";

interface ReaderToolbarProps {
  title: string;
  fullscreen: boolean;
  viewerSettings: ViewerSettings | null;
  id: number;
  token: string;
  total: number;
  getCurrentPage: () => number;
  onBack: () => void;
  onToggleFullscreen: () => void;
  onGoToPage: (pageIndex: number) => void;
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

/** A drop-down that unfolds below the top bar, right-aligned like the gear. */
function Panel({
  label,
  wide,
  onClose,
  children,
}: {
  label: string;
  wide?: boolean;
  onClose: () => void;
  children: ReactNode;
}) {
  return (
    <div
      role="dialog"
      aria-label={label}
      className={cn(
        "absolute top-full right-2 z-[1002] mt-1 space-y-4 rounded-2xl border border-kumo-hairline bg-kumo-base p-4 shadow-xl",
        wide
          ? "w-[min(30rem,calc(100vw-1rem))]"
          : "w-[min(22rem,calc(100vw-1rem))]",
      )}
    >
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold text-kumo-default">{label}</h2>
        <IconButton label="关闭" size="sm" onClick={onClose}>
          <X className="size-4" weight="bold" />
        </IconButton>
      </div>
      {children}
    </div>
  );
}

/**
 * Komga-style reader chrome: back and title on the left, reader actions on the
 * right. Each action opens an app-owned panel anchored to the button — the
 * library's own menu/pages/shortcuts/about panels are no longer used here.
 */
export function ReaderToolbar({
  title,
  fullscreen,
  viewerSettings,
  id,
  token,
  total,
  getCurrentPage,
  onBack,
  onToggleFullscreen,
  onGoToPage,
  onClosePanel,
  onUpdateSettings,
}: ReaderToolbarProps) {
  const [openPanel, setOpenPanel] = useState<ToolbarPanel | null>(null);

  const closePanel = useCallback(() => setOpenPanel(null), []);

  const togglePanel = useCallback(
    (panel: ToolbarPanel) => {
      // Close the library's own panel (reachable via its keyboard shortcuts)
      // whenever an app panel opens, so the two never overlap.
      if (openPanel !== panel) onClosePanel();
      setOpenPanel((current) => (current === panel ? null : panel));
    },
    [openPanel, onClosePanel],
  );

  // Escape closes the open panel; keep comimi's Esc-to-inline shortcut from
  // firing underneath it.
  useEffect(() => {
    if (!openPanel) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.stopPropagation();
      setOpenPanel(null);
    };
    window.addEventListener("keydown", onKeyDown, true);
    return () => window.removeEventListener("keydown", onKeyDown, true);
  }, [openPanel]);

  return (
    <>
      {openPanel && (
        <button
          type="button"
          aria-label="关闭阅读面板"
          className="fixed inset-0 z-[1000] cursor-default"
          onClick={closePanel}
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
            aria-haspopup="dialog"
            aria-expanded={openPanel === "shortcuts"}
            className="hidden sm:flex"
            onClick={() => togglePanel("shortcuts")}
          >
            <Question className="size-5" weight="bold" />
          </IconButton>

          <IconButton
            label="页面列表"
            aria-haspopup="dialog"
            aria-expanded={openPanel === "pages"}
            onClick={() => togglePanel("pages")}
          >
            <SquaresFour className="size-5" weight="bold" />
          </IconButton>

          <IconButton
            label="阅读设置"
            aria-haspopup="dialog"
            aria-expanded={openPanel === "settings"}
            onClick={() => togglePanel("settings")}
          >
            <GearSix className="size-5" weight="bold" />
          </IconButton>

          <IconButton
            label="关于 comimi"
            aria-haspopup="dialog"
            aria-expanded={openPanel === "about"}
            onClick={() => togglePanel("about")}
          >
            <ComimiMascot className="size-5 text-kumo-subtle" />
          </IconButton>
        </div>

        {openPanel === "settings" && (
          <Panel label="阅读设置" onClose={closePanel}>
            <div>
              <div className="mb-1.5 text-xs font-medium text-kumo-subtle">
                阅读方向
              </div>
              <div className="grid grid-cols-2 gap-1 rounded-lg bg-kumo-recessed p-1">
                <Segment
                  active={viewerSettings?.readingDirection === "rtl"}
                  onClick={() => onUpdateSettings({ readingDirection: "rtl" })}
                >
                  从右到左
                </Segment>
                <Segment
                  active={viewerSettings?.readingDirection === "ltr"}
                  onClick={() => onUpdateSettings({ readingDirection: "ltr" })}
                >
                  从左到右
                </Segment>
              </div>
            </div>

            <div>
              <div className="mb-1.5 text-xs font-medium text-kumo-subtle">
                翻页动画
              </div>
              <div className="grid grid-cols-2 gap-1 rounded-lg bg-kumo-recessed p-1">
                <Segment
                  active={viewerSettings?.pageTurnAnimation !== "book"}
                  onClick={() => onUpdateSettings({ pageTurnAnimation: "slide" })}
                >
                  平移
                </Segment>
                <Segment
                  active={viewerSettings?.pageTurnAnimation === "book"}
                  onClick={() => onUpdateSettings({ pageTurnAnimation: "book" })}
                >
                  书本翻页
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

          </Panel>
        )}

        {openPanel === "shortcuts" && (
          <Panel label="键盘快捷键" onClose={closePanel}>
            <ReaderShortcuts />
          </Panel>
        )}

        {openPanel === "pages" && (
          <Panel label="页面列表" wide onClose={closePanel}>
            <ReaderPageList
              id={id}
              token={token}
              total={total}
              currentPage={getCurrentPage()}
              onGoToPage={(pageIndex) => {
                onGoToPage(pageIndex);
                closePanel();
              }}
            />
          </Panel>
        )}

        {openPanel === "about" && (
          <Panel label="关于 comimi" onClose={closePanel}>
            <ReaderAbout />
          </Panel>
        )}
      </header>
    </>
  );
}
