import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  ArrowLeft,
  CornersIn,
  CornersOut,
  GearSix,
  Pause,
  Play,
  Question,
  SquaresFour,
  X,
} from "@phosphor-icons/react";
import { cn } from "@cloudflare/kumo";
import type { ViewerSettings } from "@yui540/comimi-react";
import type { ReaderSettings } from "../../lib/readerSettings";
import { useReaderPreferences } from "../../hooks/useReaderPreferences";
import { IconButton } from "../ui";
import { ReaderPageList, ReaderShortcuts } from "./ReaderToolbarPanels";

const AUTO_TURN_SECONDS = [3, 5, 8, 10, 15, 20, 30];

type ToolbarPanel = "settings" | "shortcuts" | "pages";

interface ReaderToolbarProps {
  title: string;
  fullscreen: boolean;
  overlayVisible: boolean;
  viewerSettings: ReaderSettings | null;
  id: number;
  token: string;
  total: number;
  getCurrentPage: () => number;
  autoPageTurnEnabled: boolean;
  onBack: () => void;
  onToggleFullscreen: () => void;
  onToggleAutoPageTurn: () => void;
  onGoToPage: (pageIndex: number) => void;
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
        "relative z-[1] rounded-md px-2 py-1.5 text-xs font-medium transition-colors",
        active
          ? "text-kumo-default"
          : "text-kumo-subtle hover:text-kumo-default",
      )}
    >
      {children}
    </button>
  );
}

interface SegmentOptionData {
  value: string;
  label: ReactNode;
}

function SegmentGroup({
  value,
  onChange,
  options,
}: {
  value: string;
  onChange: (value: string) => void;
  options: SegmentOptionData[];
}) {
  const groupRef = useRef<HTMLDivElement>(null);
  const indicatorRef = useRef<HTMLDivElement>(null);
  const selectedIndex = options.findIndex((option) => option.value === value);

  useLayoutEffect(() => {
    const group = groupRef.current;
    const indicator = indicatorRef.current;
    if (!group || !indicator || selectedIndex === -1) return;
    const buttons = Array.from(
      group.querySelectorAll<HTMLButtonElement>(":scope > button"),
    );
    const selected = buttons[selectedIndex];
    if (!selected) return;

    // Snap to the current value on first paint (e.g. when the settings panel
    // opens) instead of sliding in from the left edge: disable the transition,
    // apply the position, then flush the styles before restoring it so later
    // option switches still animate normally.
    const isInitial = !indicator.dataset.initialized;
    if (isInitial) {
      indicator.dataset.initialized = "true";
      indicator.style.transition = "none";
    }
    indicator.style.width = `${selected.offsetWidth}px`;
    indicator.style.transform = `translateX(${selected.offsetLeft}px)`;
    if (isInitial) {
      void indicator.offsetWidth;
      indicator.style.transition = "";
    }
  }, [selectedIndex]);

  return (
    <div ref={groupRef} className="reader-segment-group relative grid grid-cols-2 gap-1 rounded-lg bg-kumo-recessed p-1">
      <div
        ref={indicatorRef}
        className="reader-segment-indicator pointer-events-none absolute top-1 bottom-1 left-0 rounded-md bg-kumo-base shadow-sm"
        aria-hidden="true"
        style={{ width: 0, transform: "translateX(0)" }}
      />
      {options.map((option) => (
        <Segment
          key={option.value}
          active={option.value === value}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </Segment>
      ))}
    </div>
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
        "reader-panel reader-panel-enter absolute top-full right-2 z-[1002] mt-1 space-y-4 rounded-2xl border border-kumo-hairline bg-kumo-base p-4 shadow-xl",
        wide
          ? "w-[min(30rem,calc(100vw-1rem))]"
          : "w-[min(22rem,calc(100vw-1rem))]",
      )}
      onAnimationEnd={(event) => {
        event.currentTarget.classList.remove("reader-panel-enter");
      }}
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
 * right. Each action opens an app-owned panel anchored to the button.
 */
export function ReaderToolbar({
  title,
  fullscreen,
  overlayVisible,
  viewerSettings,
  id,
  token,
  total,
  getCurrentPage,
  autoPageTurnEnabled,
  onBack,
  onToggleFullscreen,
  onToggleAutoPageTurn,
  onGoToPage,
  onUpdateSettings,
}: ReaderToolbarProps) {
  const [openPanel, setOpenPanel] = useState<ToolbarPanel | null>(null);
  const { autoFullscreenOnStart, setAutoFullscreenOnStart } =
    useReaderPreferences();

  const closePanel = useCallback(() => setOpenPanel(null), []);

  const togglePanel = useCallback((panel: ToolbarPanel) => {
    setOpenPanel((current) => (current === panel ? null : panel));
  }, []);

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

  const intervalSeconds = viewerSettings
    ? Math.round(viewerSettings.autoPageTurnIntervalMs / 1000)
    : 5;

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
        data-visible={overlayVisible || openPanel !== null}
        className="reader-topbar flex items-center gap-1 px-2 py-1.5"
      >
        <IconButton label="返回画廊" onClick={onBack}>
          <ArrowLeft className="size-5" weight="bold" />
        </IconButton>

        <h1 className="min-w-0 flex-1 truncate px-1 text-[15px] font-semibold text-kumo-default">
          {title}
        </h1>

        <div className="flex items-center gap-0.5">
          <IconButton
            label={autoPageTurnEnabled ? "停止自动播放" : "自动播放"}
            aria-pressed={autoPageTurnEnabled}
            onClick={onToggleAutoPageTurn}
          >
            {autoPageTurnEnabled ? (
              <Pause className="size-5" weight="bold" />
            ) : (
              <Play className="size-5" weight="bold" />
            )}
          </IconButton>

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
        </div>

        {openPanel === "settings" && (
          <Panel label="阅读设置" onClose={closePanel}>
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
                  onClick={() => setAutoFullscreenOnStart(!autoFullscreenOnStart)}
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
              <SegmentGroup
                value={viewerSettings?.readingDirection ?? "rtl"}
                onChange={(value) =>
                  onUpdateSettings({
                    readingDirection: value as "rtl" | "ltr",
                  })
                }
                options={[
                  { value: "rtl", label: "从右到左" },
                  { value: "ltr", label: "从左到右" },
                ]}
              />
            </div>

            <div>
              <div className="mb-1.5 text-xs font-medium text-kumo-subtle">
                翻页模式
              </div>
              <SegmentGroup
                value={viewerSettings?.pageTurnMode ?? "single"}
                onChange={(value) =>
                  onUpdateSettings({
                    pageTurnMode: value as "single" | "spread",
                  })
                }
                options={[
                  { value: "single", label: "单页" },
                  { value: "spread", label: "双页" },
                ]}
              />
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
      </header>
    </>
  );
}
