import { useEffect, useState, type RefObject } from "react";

// Used before the first layout measurement (and in environments without
// ResizeObserver, e.g. tests): must match the `.gallery-grid` breakpoints in
// `index.css`.
function columnsForWidth(width: number): number {
  if (width < 480) return 2;
  if (width < 768) return 3;
  if (width < 1024) return 4;
  if (width < 1440) return 5;
  if (width < 1920) return 6;
  return 7;
}

function measureColumns(element: HTMLElement): number {
  // The computed value is one track size per column ("240px 240px …"), even
  // while the grid is empty. Reading it avoids guessing from the window width,
  // which can disagree with the real layout (scrollbars, zoom) and leave a
  // placeholder row with the wrong number of cells.
  const template = window.getComputedStyle(element).gridTemplateColumns;
  if (!template || template === "none") return 0;
  return template.split(" ").filter(Boolean).length;
}

/**
 * Live column count of the `.gallery-grid` behind `ref`, measured from the
 * layout itself so a placeholder row always has one cell per column.
 */
export function useGridColumns(ref?: RefObject<HTMLElement | null>): number {
  const [columns, setColumns] = useState(() =>
    columnsForWidth(window.innerWidth),
  );

  useEffect(() => {
    const element = ref?.current ?? null;

    const update = () => {
      const measured = element ? measureColumns(element) : 0;
      setColumns(measured > 0 ? measured : columnsForWidth(window.innerWidth));
    };

    update();

    if (element && typeof ResizeObserver !== "undefined") {
      // The grid is fluid, so a breakpoint change also resizes the element.
      const observer = new ResizeObserver(update);
      observer.observe(element);
      return () => observer.disconnect();
    }

    window.addEventListener("resize", update, { passive: true });
    return () => window.removeEventListener("resize", update);
  }, [ref]);

  return columns;
}
