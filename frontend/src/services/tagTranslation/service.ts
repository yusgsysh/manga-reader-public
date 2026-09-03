import type { Tag } from "../../types/gallery";
import { loadDb, updateDb } from "./loader";
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
  private updateStatus: TranslationStatus = "idle";
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

  getUpdateStatus = (): TranslationStatus => this.updateStatus;

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
    const raw = await loadDb();
    this.applyIndex(parseDb(raw));
    this.status = "ready";
    this.notify();
  }

  loadFromData(data: unknown): void {
    this.applyIndex(parseDb(data));
  }

  async update(): Promise<{ changed: boolean }> {
    const previousSha = this.info?.sha;
    this.updateStatus = "loading";
    this.notify();
    try {
      const raw = await updateDb();
      this.applyIndex(parseDb(raw));
      if (this.status !== "ready") {
        this.status = "ready";
      }
      this.updateStatus = "ready";
      this.notify();
      return { changed: this.info?.sha !== previousSha };
    } catch (error) {
      this.updateStatus = "error";
      this.errorMessage =
        error instanceof Error ? error.message : String(error);
      this.notify();
      throw error;
    }
  }

  private applyIndex(index: ReturnType<typeof parseDb>) {
    this.translationMap = index.translationMap;
    this.namespaceMap = index.namespaceMap;
    this.info = {
      version: index.version,
      sha: index.sha,
      loadedAt: Date.now(),
    };
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

  searchByTranslation(query: string, limit = 10): TagTranslationEntry[] {
    if (!query || this.status !== "ready") return [];
    const lowerQuery = query.toLowerCase();
    const results: TagTranslationEntry[] = [];
    for (const entry of this.translationMap.values()) {
      if (entry.translation.toLowerCase().includes(lowerQuery)) {
        results.push(entry);
        if (results.length >= limit) break;
      }
    }
    return results;
  }

  searchTags(query: string, limit = 10): Array<{ namespace: string; tag: string; translation: string }> {
    if (!query || this.status !== "ready") return [];
    const lowerQuery = query.toLowerCase();
    const results: Array<{ namespace: string; tag: string; translation: string }> = [];
    for (const entry of this.translationMap.values()) {
      if (
        entry.translation.toLowerCase().includes(lowerQuery) ||
        entry.tag.toLowerCase().includes(lowerQuery)
      ) {
        results.push({
          namespace: entry.namespace,
          tag: entry.tag,
          translation: entry.translation,
        });
        if (results.length >= limit) break;
      }
    }
    return results;
  }
}

export const tagTranslationService = new TagTranslationService();
