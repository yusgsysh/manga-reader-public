// Mirrors ExHentai's inline Jump/Seek parser (ehg_index.c.js): a bare year or a
// dashed date is a "seek" (absolute date), anything else matching a number with
// an optional d/w/m/y (or "-") suffix is a relative "jump".

const DATE_RE = /^\d{2,4}-\d{1,2}(-\d{1,2})?$/;
const YEAR_RE = /^\d{4}$/;
const JUMP_RE = /^\d+[dwmy-]?$/;

type JumpSeek = { kind: "seek"; value: string } | { kind: "jump"; value: string };

function isSeek(value: string): boolean {
  if (DATE_RE.test(value)) return true;
  if (YEAR_RE.test(value)) {
    const year = Number(value);
    return year > 2006 && year < 2100;
  }
  return false;
}

/**
 * Classifies a Jump/Seek input. Returns null for empty or invalid input.
 */
export function parseJumpSeekInput(input: string): JumpSeek | null {
  const value = input.trim();
  if (!value) return null;
  if (isSeek(value)) return { kind: "seek", value };
  if (JUMP_RE.test(value)) return { kind: "jump", value };
  return null;
}
