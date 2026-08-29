import type { Tag } from "../../types/gallery";
import type { TagTranslationEntry, TranslationIndex } from "./types";

interface RawDbEntry {
  name?: string;
  intro?: string;
}

interface RawDbNamespace {
  namespace: string;
  data?: Record<string, RawDbEntry>;
}

interface RawDbRoot {
  version?: string | number;
  data?: RawDbNamespace[];
}

const NAMESPACE_TRANSLATIONS_KEY = "rows";

export function getTagKey(tag: Pick<Tag, "namespace" | "name">): string {
  return `${tag.namespace}:${tag.name}`;
}

function cleanText(text: string): string {
  if (text.indexOf("<") === -1) return text.trim();
  return text.replace(/<[^>]*>/g, " ").replace(/\s+/g, " ").trim();
}

export function parseDb(data: unknown): TranslationIndex {
  const root = data as RawDbRoot;
  const translationMap = new Map<string, TagTranslationEntry>();
  const namespaceMap = new Map<string, string>();

  for (const ns of root.data ?? []) {
    const namespace = ns.namespace;
    const entries = ns.data ?? {};

    for (const [tagName, info] of Object.entries(entries)) {
      const translation = cleanText(info.name ?? "");
      if (!translation) continue;

      translationMap.set(getTagKey({ namespace, name: tagName }), {
        namespace,
        tag: tagName,
        translation,
        description: info.intro ? cleanText(info.intro) : undefined,
      });
    }

    if (namespace === NAMESPACE_TRANSLATIONS_KEY) {
      for (const [tagName, info] of Object.entries(entries)) {
        const translation = cleanText(info.name ?? "");
        if (translation) namespaceMap.set(tagName, translation);
      }
    }
  }

  return {
    translationMap,
    namespaceMap,
    version: root.version?.toString(),
  };
}
