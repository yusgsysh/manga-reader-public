import type { Tag } from "../types/gallery";

export function parseTagString(raw: string): Tag {
  const idx = raw.indexOf(":");
  if (idx > 0) {
    return { namespace: raw.slice(0, idx), name: raw.slice(idx + 1) };
  }
  return { namespace: "", name: raw };
}
