/**
 * Reader preferences that belong to the app rather than to comimi.
 *
 * comimi keeps its own viewer settings in memory (storage is disabled), so the
 * few choices the reader chrome owns live here, in localStorage, and stay in
 * sync between the reader panel and the settings page.
 */

export interface ReaderPreferences {
  /** Enter the reader in fullscreen when it opens from a gallery. */
  autoFullscreenOnStart: boolean;
}

export const READER_PREFERENCES_STORAGE_KEY =
  "manga-reader:reader-preferences";

const CHANGE_EVENT = "manga-reader:reader-preferences-change";

export const DEFAULT_READER_PREFERENCES: ReaderPreferences = Object.freeze({
  autoFullscreenOnStart: false,
});

function normalize(value: unknown): ReaderPreferences {
  const raw = (value ?? {}) as Record<string, unknown>;
  return {
    autoFullscreenOnStart: raw.autoFullscreenOnStart === true,
  };
}

let cached: ReaderPreferences | null = null;

function readFromStorage(): ReaderPreferences {
  if (typeof window === "undefined") return DEFAULT_READER_PREFERENCES;
  try {
    const raw = window.localStorage.getItem(READER_PREFERENCES_STORAGE_KEY);
    if (!raw) return DEFAULT_READER_PREFERENCES;
    return normalize(JSON.parse(raw));
  } catch {
    // A corrupt value or a blocked storage API must not break the reader.
    return DEFAULT_READER_PREFERENCES;
  }
}

export function getReaderPreferences(): ReaderPreferences {
  if (!cached) cached = readFromStorage();
  return cached;
}

export function updateReaderPreferences(
  patch: Partial<ReaderPreferences>,
): ReaderPreferences {
  const next = normalize({ ...getReaderPreferences(), ...patch });
  cached = next;
  if (typeof window !== "undefined") {
    try {
      window.localStorage.setItem(
        READER_PREFERENCES_STORAGE_KEY,
        JSON.stringify(next),
      );
    } catch {
      // Private mode / quota denied: keep the in-memory value for this tab.
    }
    window.dispatchEvent(new Event(CHANGE_EVENT));
  }
  return next;
}

export function subscribeReaderPreferences(listener: () => void): () => void {
  if (typeof window === "undefined") return () => undefined;
  const handleStorage = (event: StorageEvent) => {
    if (event.key !== READER_PREFERENCES_STORAGE_KEY) return;
    cached = null;
    listener();
  };
  window.addEventListener("storage", handleStorage);
  window.addEventListener(CHANGE_EVENT, listener);
  return () => {
    window.removeEventListener("storage", handleStorage);
    window.removeEventListener(CHANGE_EVENT, listener);
  };
}

/**
 * Ask the browser for native fullscreen as part of the click that starts
 * reading. Called from the gallery's start-reading controls, where a user
 * gesture is still available.
 */
export function requestReaderFullscreenIfEnabled(): void {
  if (!getReaderPreferences().autoFullscreenOnStart) return;
  if (typeof document === "undefined") return;
  const element = document.documentElement;
  if (!element || typeof element.requestFullscreen !== "function") return;
  if (document.fullscreenElement) return;
  void element
    .requestFullscreen({ navigationUI: "hide" })
    .catch(() => undefined);
}

/** Test helper: drop the cached snapshot after localStorage is changed. */
export function resetReaderPreferencesCache(): void {
  cached = null;
}
