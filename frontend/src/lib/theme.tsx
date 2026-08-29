import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { ThemeContext } from "../hooks/useTheme";

export type ThemeMode = "light" | "dark" | "system";

const STORAGE_KEY = "manga-reader-theme";

function getInitialMode(): ThemeMode {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === "light" || stored === "dark" || stored === "system") {
    return stored;
  }
  return "system";
}

function resolveMode(mode: ThemeMode): "light" | "dark" {
  if (mode !== "system") return mode;
  return window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}

function applyMode(mode: ThemeMode) {
  const resolved = resolveMode(mode);
  document.documentElement.setAttribute("data-mode", resolved);
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(getInitialMode);

  useEffect(() => {
    applyMode(mode);
    localStorage.setItem(STORAGE_KEY, mode);

    if (mode === "system") {
      const mql = window.matchMedia("(prefers-color-scheme: dark)");
      const listener = () => applyMode("system");
      mql.addEventListener("change", listener);
      return () => mql.removeEventListener("change", listener);
    }
  }, [mode]);

  const setMode = useCallback((next: ThemeMode) => {
    setModeState(next);
  }, []);

  const value = useMemo(() => ({ mode, setMode }), [mode, setMode]);

  return (
    <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
  );
}
