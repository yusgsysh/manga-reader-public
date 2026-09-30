import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { ThemeContext, type ResolvedTheme } from "../hooks/useTheme";

export type ThemeMode = "light" | "dark" | "system";

const STORAGE_KEY = "manga-reader-theme";

function getInitialMode(): ThemeMode {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === "light" || stored === "dark" || stored === "system") {
    return stored;
  }
  return "light";
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(getInitialMode);
  const [systemPrefersDark, setSystemPrefersDark] = useState(
    () => window.matchMedia("(prefers-color-scheme: dark)").matches,
  );

  const resolvedMode: ResolvedTheme =
    mode === "system" ? (systemPrefersDark ? "dark" : "light") : mode;

  useEffect(() => {
    const mql = window.matchMedia("(prefers-color-scheme: dark)");
    const listener = (e: MediaQueryListEvent) => setSystemPrefersDark(e.matches);
    mql.addEventListener("change", listener);
    return () => mql.removeEventListener("change", listener);
  }, []);

  useEffect(() => {
    document.documentElement.setAttribute("data-mode", resolvedMode);
    localStorage.setItem(STORAGE_KEY, mode);
  }, [mode, resolvedMode]);

  const setMode = useCallback((next: ThemeMode) => {
    setModeState(next);
  }, []);

  const value = useMemo(
    () => ({ mode, resolvedMode, setMode }),
    [mode, resolvedMode, setMode],
  );

  return (
    <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
  );
}
