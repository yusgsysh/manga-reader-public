import { useEffect, useState } from "react";

// Must match the `.gallery-grid` breakpoints in `index.css`: skeleton rows are
// rendered as grid children, so a wrong count would leave the row ragged.
function columnsForWidth(width: number): number {
  if (width < 480) return 2;
  if (width < 768) return 3;
  if (width < 1024) return 4;
  if (width < 1440) return 5;
  if (width < 1920) return 6;
  return 7;
}

/** Live column count of `.gallery-grid`, for sizing placeholder rows. */
export function useGridColumns(): number {
  const [columns, setColumns] = useState(() =>
    columnsForWidth(window.innerWidth),
  );

  useEffect(() => {
    const onResize = () => setColumns(columnsForWidth(window.innerWidth));
    window.addEventListener("resize", onResize, { passive: true });
    return () => window.removeEventListener("resize", onResize);
  }, []);

  return columns;
}
