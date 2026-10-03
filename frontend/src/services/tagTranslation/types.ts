export type TranslationStatus = "idle" | "loading" | "ready" | "error";

export interface TagTranslationEntry {
  namespace: string;
  tag: string;
  translation: string;
  description?: string;
  /**
   * Lowercased `translation + "\0" + tag`, precomputed once when the database
   * is parsed so suggestion searches never re-allocate ~88k strings per call.
   */
  searchKey: string;
}

export interface TagTranslationDatabaseInfo {
  version?: string;
  sha?: string;
  loadedAt: number;
}

export interface TranslationIndex {
  translationMap: Map<string, TagTranslationEntry>;
  namespaceMap: Map<string, string>;
  version?: string;
  sha?: string;
}
