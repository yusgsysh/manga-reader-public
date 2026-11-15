import { useState } from "react";

export interface ReaderControlsBarProps {
  /** Current comic page (0-based). */
  page: number;
  total: number;
  /** Mirrors the active viewer's auto-hiding overlay. */
  visible: boolean;
  readingDirection: "rtl" | "ltr";
  onGoToPage: (pageIndex: number) => void;
}

/**
 * Book mode's progress control.
 *
 * Slide mode keeps comimi's own dock; the book viewer has no comimi instance,
 * so this renders the same dock markup and classes (`.comimi-seek`), letting
 * the library's stylesheet draw the identical page number + seek bar. The
 * theme variables comimi normally inherits from `.comimi-root` are supplied in
 * `index.css` on `.reader-book-dock`.
 */
export function ReaderControlsBar({
  page,
  total,
  visible,
  readingDirection,
  onGoToPage,
}: ReaderControlsBarProps) {
  const [scrub, setScrub] = useState<number | null>(null);
  const maxPage = Math.max(0, total - 1);
  const value = Math.min(scrub ?? page, maxPage);
  const progress = total <= 1 ? 0 : (value / maxPage) * 100;

  const commitScrub = () => {
    if (scrub !== null) onGoToPage(scrub);
    setScrub(null);
  };

  return (
    <div
      className="comimi-controls-dock reader-book-dock"
      data-overlay={visible}
      inert={!visible}
    >
      <div className="comimi-controls-bg" />
      <div className="comimi-seek">
        <div className="comimi-seek-text">
          <span className="comimi-seek-current">{page + 1}</span>
          <span className="comimi-seek-total"> / {total}</span>
        </div>
        <div className="comimi-seek-bar" data-direction={readingDirection}>
          <div className="comimi-seek-track">
            <div
              className="comimi-seek-fill"
              style={{ width: `${progress}%` }}
            />
          </div>
          <input
            type="range"
            aria-label="跳转页码"
            className="comimi-seek-input"
            data-direction={readingDirection}
            min={0}
            max={maxPage}
            value={value}
            onChange={(event) => setScrub(Number(event.target.value))}
            onPointerUp={commitScrub}
            onKeyUp={commitScrub}
            onBlur={() => setScrub(null)}
          />
        </div>
      </div>
    </div>
  );
}
