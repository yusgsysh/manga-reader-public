import type { Tag } from "../../types/gallery";
import { loadDbHtmlJs } from "./loader";
import { getTagKey, parseDb } from "./parser";
import type {
  TagTranslationDatabaseInfo,
  TagTranslationEntry,
  TranslationStatus,
} from "./types";

class TagTranslationService {
  private translationMap = new Map<string, TagTranslationEntry>();
  private namespaceMap = new Map<string, string>();
  private status: TranslationStatus = "idle";
  private errorMessage: string | null = null;
  private loadPromise: Promise<void> | null = null;
  private listeners = new Set<() => void>();
  private info: TagTranslationDatabaseInfo | null = null;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getStatus = (): TranslationStatus => this.status;

  getErrorMessage = (): string | null => this.errorMessage;

  getInfo = (): TagTranslationDatabaseInfo | null => this.info;

  private notify() {
    for (const listener of this.listeners) {
      listener();
    }
  }

  load(): Promise<void> {
    if (this.loadPromise) return this.loadPromise;
    this.status = "loading";
    this.notify();
    this.loadPromise = this.doLoad().catch((error: unknown) => {
      this.status = "error";
      this.errorMessage =
        error instanceof Error ? error.message : String(error);
      this.notify();
      console.error("Failed to load EhTagTranslation database", error);
    });
    return this.loadPromise;
  }

  private async doLoad(): Promise<void> {
    const raw = await loadDbHtmlJs();
    this.applyIndex(parseDb(raw));
    this.status = "ready";
    this.notify();
  }

  loadFromData(data: unknown): void {
    this.applyIndex(parseDb(data));
  }

  private applyIndex(index: ReturnType<typeof parseDb>) {
    this.translationMap = index.translationMap;
    this.namespaceMap = index.namespaceMap;
    this.info = { version: index.version, loadedAt: Date.now() };
  }

  lookup(tag: Tag): TagTranslationEntry | undefined {
    const exact = this.translationMap.get(getTagKey(tag));
    if (exact) return exact;
    return this.translationMap.get(
      getTagKey({ namespace: tag.namespace, name: tag.name.toLowerCase() }),
    );
  }

  translateTag(tag: Tag): string {
    const entry = this.lookup(tag);
    if (entry) return entry.translation;
    return tag.namespace ? `${tag.namespace}:${tag.name}` : tag.name;
  }

  hasTranslation(tag: Tag): boolean {
    return this.lookup(tag) !== undefined;
  }

  translateNamespace(namespace: string): string {
    if (!namespace) return "";
    const exact = this.namespaceMap.get(namespace);
    if (exact) return exact;
    return this.namespaceMap.get(namespace.toLowerCase()) ?? namespace;
  }
}

export const tagTranslationService = new TagTranslationService();
