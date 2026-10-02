import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { ThemeContext, type ResolvedTheme } from "../hooks/useTheme";
import {
  ACCENT_STORAGE_KEY,
  accentContrast,
  isValidHexColor,
  normalizeAccent,
} from "./accent";

export type ThemeMode = "light" | "dark" | "system";

const STORAGE_KEY = "manga-reader-theme";

const ACCENT_PROPS = [
  "--app-accent",
  "--app-accent-contrast",
  "--color-kumo-brand",
  "--color-kumo-brand-hover",
] as const;

function getInitialMode(): ThemeMode {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === "light" || stored === "dark" || stored === "system") {
    return stored;
  }
  return "light";
}

function getInitialAccent(): string | null {
  const stored = localStorage.getItem(ACCENT_STORAGE_KEY);
  return stored && isValidHexColor(stored) ? normalizeAccent(stored) : null;
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(getInitialMode);
  const [accent, setAccentState] = useState<string | null>(getInitialAccent);
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

  // Apply the accent override on the root element. Derived tokens such as
  // --app-accent-soft and --app-ring are color-mix() expressions over
  // --app-accent, so they follow automatically. Removing the properties falls
  // back to the per-mode defaults defined in index.css.
  useLayoutEffect(() => {
    const root = document.documentElement;
    if (!accent) {
      for (const prop of ACCENT_PROPS) root.style.removeProperty(prop);
      return;
    }
    root.style.setProperty("--app-accent", accent);
    root.style.setProperty("--app-accent-contrast", accentContrast(accent));
    root.style.setProperty("--color-kumo-brand", accent);
    root.style.setProperty(
      "--color-kumo-brand-hover",
      `color-mix(in oklab, ${accent} 82%, ${resolvedMode === "dark" ? "white" : "black"})`,
    );
  }, [accent, resolvedMode]);

  const setMode = useCallback((next: ThemeMode) => {
    setModeState(next);
  }, []);

  const setAccent = useCallback((next: string | null) => {
    setAccentState(next ? normalizeAccent(next) : null);
  }, []);

  useEffect(() => {
    if (accent) localStorage.setItem(ACCENT_STORAGE_KEY, accent);
    else localStorage.removeItem(ACCENT_STORAGE_KEY);
  }, [accent]);

  const value = useMemo(
    () => ({ mode, resolvedMode, setMode, accent, setAccent }),
    [mode, resolvedMode, setMode, accent, setAccent],
  );

  return (
    <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
  );
}
