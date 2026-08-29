export type TranslationStatus = "idle" | "loading" | "ready" | "error";

export interface TagTranslationEntry {
  namespace: string;
  tag: string;
  translation: string;
  description?: string;
}

export interface TagTranslationDatabaseInfo {
  version?: string;
  loadedAt: number;
}

export interface TranslationIndex {
  translationMap: Map<string, TagTranslationEntry>;
  namespaceMap: Map<string, string>;
  version?: string;
}
