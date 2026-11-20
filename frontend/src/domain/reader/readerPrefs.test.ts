import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  DEFAULT_READER_PREFERENCES,
  READER_PREFERENCES_STORAGE_KEY,
  getReaderPreferences,
  requestReaderFullscreenIfEnabled,
  resetReaderPreferencesCache,
  subscribeReaderPreferences,
  updateReaderPreferences,
} from "./readerPrefs";

function installWindow(storage: Record<string, string> = {}) {
  const listeners = new Set<(event: unknown) => void>();
  const win = {
    localStorage: {
      getItem: (key: string) => storage[key] ?? null,
      setItem: (key: string, value: string) => {
        storage[key] = value;
      },
    },
    addEventListener: (_type: string, listener: (event: unknown) => void) =>
      listeners.add(listener),
    removeEventListener: (_type: string, listener: (event: unknown) => void) =>
      listeners.delete(listener),
    dispatchEvent: (event: unknown) => {
      for (const listener of listeners) listener(event);
      return true;
    },
  };
  (globalThis as { window?: unknown }).window = win;
  return win;
}

beforeEach(() => {
  delete (globalThis as { window?: unknown }).window;
  delete (globalThis as { document?: unknown }).document;
  resetReaderPreferencesCache();
});

describe("reader preferences", () => {
  it("defaults auto-fullscreen off when nothing is stored", () => {
    installWindow();
    expect(getReaderPreferences()).toEqual(DEFAULT_READER_PREFERENCES);
  });

  it("persists an update and reads it back", () => {
    const storage: Record<string, string> = {};
    installWindow(storage);

    updateReaderPreferences({ autoFullscreenOnStart: true });

    expect(getReaderPreferences().autoFullscreenOnStart).toBe(true);
    expect(
      JSON.parse(storage[READER_PREFERENCES_STORAGE_KEY]),
    ).toMatchObject({ autoFullscreenOnStart: true });
  });

  it("falls back to defaults on a corrupt stored value", () => {
    installWindow({ [READER_PREFERENCES_STORAGE_KEY]: "{not json" });
    expect(getReaderPreferences()).toEqual(DEFAULT_READER_PREFERENCES);
  });

  it("coerces unknown fields to the strict shape", () => {
    installWindow({
      [READER_PREFERENCES_STORAGE_KEY]: JSON.stringify({ other: 1 }),
    });
    expect(getReaderPreferences().autoFullscreenOnStart).toBe(false);
  });

  it("notifies subscribers on update", () => {
    installWindow();
    const listener = vi.fn();
    const unsubscribe = subscribeReaderPreferences(listener);

    updateReaderPreferences({ autoFullscreenOnStart: true });

    expect(listener).toHaveBeenCalledTimes(1);
    unsubscribe();
    updateReaderPreferences({ autoFullscreenOnStart: false });
    expect(listener).toHaveBeenCalledTimes(1);
  });
});

describe("requestReaderFullscreenIfEnabled", () => {
  it("does nothing when the preference is off", () => {
    installWindow();
    const request = vi.fn().mockResolvedValue(undefined);
    (globalThis as { document?: unknown }).document = {
      documentElement: { requestFullscreen: request },
      fullscreenElement: null,
    };

    requestReaderFullscreenIfEnabled();

    expect(request).not.toHaveBeenCalled();
  });

  it("requests fullscreen from the start-reading click when enabled", () => {
    installWindow();
    updateReaderPreferences({ autoFullscreenOnStart: true });
    const request = vi.fn().mockResolvedValue(undefined);
    (globalThis as { document?: unknown }).document = {
      documentElement: { requestFullscreen: request },
      fullscreenElement: null,
    };

    requestReaderFullscreenIfEnabled();

    expect(request).toHaveBeenCalledWith({ navigationUI: "hide" });
  });
});
