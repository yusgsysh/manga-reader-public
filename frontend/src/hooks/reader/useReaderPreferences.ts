import { useSyncExternalStore } from "react";
import {
  getReaderPreferences,
  subscribeReaderPreferences,
  updateReaderPreferences,
} from "../../domain/reader/readerPrefs";

/** Reader preferences plus setters, shared across every consumer in the tab. */
export function useReaderPreferences() {
  const preferences = useSyncExternalStore(
    subscribeReaderPreferences,
    getReaderPreferences,
    getReaderPreferences,
  );

  return {
    ...preferences,
    setAutoFullscreenOnStart: (enabled: boolean) => {
      updateReaderPreferences({ autoFullscreenOnStart: enabled });
    },
  };
}
