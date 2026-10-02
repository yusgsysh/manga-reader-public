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
  mixHex,
  normalizeAccent,
  resolveAccent,
} from "./accent";

export type ThemeMode = "light" | "dark" | "system";

const STORAGE_KEY = "manga-reader-theme";

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

  // Apply the accent override on the root element. Every accent (including the
  // default) resolves to a concrete per-mode color, so the behavior is uniform.
  // Derived tokens such as --app-accent-soft and --app-ring are color-mix()
  // expressions over --app-accent, so they follow automatically.
  const appliedAccent = resolveAccent(accent, resolvedMode);

  useLayoutEffect(() => {
    const root = document.documentElement;
    root.style.setProperty("--app-accent", appliedAccent);
    root.style.setProperty(
      "--app-accent-contrast",
      accentContrast(appliedAccent),
    );
    root.style.setProperty("--color-kumo-brand", appliedAccent);
    root.style.setProperty(
      "--color-kumo-brand-hover",
      mixHex(
        appliedAccent,
        resolvedMode === "dark" ? "#ffffff" : "#000000",
        0.18,
      ),
    );
  }, [appliedAccent, resolvedMode]);

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
