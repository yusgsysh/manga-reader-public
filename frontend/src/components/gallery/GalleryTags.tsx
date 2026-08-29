import type { Tag } from "../../types/gallery";
import { parseTagString } from "../../lib/tag";
import { TagList } from "../tag";

interface GalleryTagsProps {
  tags?: string[];
  max?: number;
}

export function GalleryTags({ tags, max = 3 }: GalleryTagsProps) {
  const parsed: Tag[] = (tags ?? []).map(parseTagString);
  return <TagList tags={parsed} max={max} />;
}
