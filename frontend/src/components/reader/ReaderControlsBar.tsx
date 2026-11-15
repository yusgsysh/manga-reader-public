import { useState } from "react";
import { CaretLeft, CaretRight } from "@phosphor-icons/react";

export interface ReaderControlsBarProps {
  /** Current comic page (0-based). */
  page: number;
  total: number;
  /** Mirrors the active viewer's auto-hiding overlay. */
  visible: boolean;
  readingDirection: "rtl" | "ltr";
  canPrev: boolean;
  canNext: boolean;
  onTurnPage: (direction: "previous" | "next") => void;
  onGoToPage: (pageIndex: number) => void;
}

/**
 * The one control bar both reading modes share: slide (comimi) and book
 * (grabfold) render the same prev/page/slider/next row, positioned and hidden
 * exactly like the top bar. The library's own dock and side arrows are hidden
 * in the reader shell, so a mode switch never swaps one control set for
 * another.
 */
export function ReaderControlsBar({
  page,
  total,
  visible,
  readingDirection,
  canPrev,
  canNext,
  onTurnPage,
  onGoToPage,
}: ReaderControlsBarProps) {
  const [scrub, setScrub] = useState<number | null>(null);
  const maxPage = Math.max(0, total - 1);
  const value = Math.min(scrub ?? page, maxPage);

  const commitScrub = () => {
    if (scrub !== null) onGoToPage(scrub);
    setScrub(null);
  };

  return (
    <div
      data-reader-chrome
      data-visible={visible}
      inert={!visible}
      aria-label="阅读控件"
      className="reader-controls-bar pointer-events-auto absolute left-1/2 z-[1001] flex -translate-x-1/2 items-center gap-1.5 rounded-full border border-kumo-hairline bg-kumo-base/95 px-2 py-1.5 shadow-lg backdrop-blur"
      style={{ bottom: "max(0.75rem, env(safe-area-inset-bottom, 0px))" }}
    >
      <button
        type="button"
        aria-label="上一页"
        disabled={!canPrev}
        onClick={() => onTurnPage("previous")}
        className="grid size-8 place-items-center rounded-full text-kumo-subtle transition-colors hover:bg-kumo-recessed hover:text-kumo-default disabled:opacity-35 disabled:hover:bg-transparent"
      >
        <CaretLeft className="size-4" weight="bold" />
      </button>
      <span className="flex items-center gap-1 text-xs tabular-nums text-kumo-default">
        <PageNumberInput page={page} total={total} onGoToPage={onGoToPage} />
        <span className="text-kumo-subtle">/ {total}</span>
      </span>
      <input
        type="range"
        aria-label="跳转页码"
        dir={readingDirection === "rtl" ? "rtl" : "ltr"}
        min={0}
        max={maxPage}
        value={value}
        onChange={(event) => setScrub(Number(event.target.value))}
        onPointerUp={commitScrub}
        onKeyUp={commitScrub}
        onBlur={() => setScrub(null)}
        className="reader-book-seek h-1 w-32 cursor-pointer appearance-none rounded-full bg-kumo-recessed accent-[var(--app-accent)] sm:w-48"
      />
      <button
        type="button"
        aria-label="下一页"
        disabled={!canNext}
        onClick={() => onTurnPage("next")}
        className="grid size-8 place-items-center rounded-full text-kumo-subtle transition-colors hover:bg-kumo-recessed hover:text-kumo-default disabled:opacity-35 disabled:hover:bg-transparent"
      >
        <CaretRight className="size-4" weight="bold" />
      </button>
    </div>
  );
}

/** A page number the reader can type, like the paged reader's seek input. */
function PageNumberInput({
  page,
  total,
  onGoToPage,
}: {
  page: number;
  total: number;
  onGoToPage: (pageIndex: number) => void;
}) {
  // Follow the viewer until the reader starts typing. A turn that lands while a
  // draft is being typed replaces it, so the field can never lie about where
  // the reader is.
  const [field, setField] = useState({ page, draft: String(page + 1) });
  if (field.page !== page) {
    setField({ page, draft: String(page + 1) });
  }
  const draft = field.page === page ? field.draft : String(page + 1);

  const commit = () => {
    const wanted = Number(draft);
    if (!Number.isFinite(wanted) || draft === "") {
      setField({ page, draft: String(page + 1) });
      return;
    }
    const clamped = Math.min(Math.max(Math.round(wanted) - 1, 0), total - 1);
    setField({ page, draft: String(clamped + 1) });
    if (clamped !== page) onGoToPage(clamped);
  };

  return (
    <input
      type="text"
      inputMode="numeric"
      aria-label="当前页码"
      value={draft}
      onChange={(event) =>
        setField({ page, draft: event.target.value.replace(/\D/g, "") })
      }
      onBlur={commit}
      onKeyDown={(event) => {
        if (event.key !== "Enter") return;
        event.preventDefault();
        commit();
        event.currentTarget.blur();
      }}
      className="w-9 rounded border border-transparent bg-transparent text-right text-xs tabular-nums text-kumo-default outline-none focus:border-kumo-hairline focus:bg-kumo-recessed"
    />
  );
}
