import { useCallback } from "react";
import { useNavigate } from "react-router";

/**
 * Returns a back handler that pops the history stack when there is an earlier
 * entry, and otherwise replaces the current entry with `fallback`.
 *
 * The reader pushes a gallery fallback when it was opened directly (deep link /
 * browser history). Pushing instead of replacing leaves the reader in the back
 * stack, so the gallery's back bounces straight back to the reader — an endless
 * gallery ↔ reader loop. Replacing breaks that cycle, and mirrors it on the
 * gallery so a direct-loaded page falls back to home instead of an unrelated
 * entry.
 *
 * We key off React Router's `history.state.idx` rather than `navigationType`:
 * the reader strips its `?page=` deep-link param with a REPLACE navigation right
 * after mount, which would make `navigationType` report REPLACE even though the
 * reader was genuinely pushed from the gallery.
 */
export function useBackNavigation(fallback: string) {
  const navigate = useNavigate();

  return useCallback(() => {
    const index = window.history.state?.idx;
    if (typeof index === "number" && index > 0) {
      navigate(-1);
    } else {
      navigate(fallback, { replace: true });
    }
  }, [navigate, fallback]);
}
