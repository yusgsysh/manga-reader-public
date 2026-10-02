interface AccentPreset {
  id: string;
  label: string;
  color: string;
}

// Preset theme colors. Choosing one overrides the default per-mode accent.
export const ACCENT_PRESETS: AccentPreset[] = [
  { id: "green", label: "绿色", color: "#16a34a" },
  { id: "teal", label: "青色", color: "#0891b2" },
  { id: "violet", label: "紫色", color: "#7c3aed" },
  { id: "pink", label: "粉色", color: "#db2777" },
  { id: "orange", label: "橙色", color: "#ea580c" },
  { id: "rose", label: "玫红", color: "#e11d48" },
];

// The built-in accent (matches the light-mode stylesheet default). Applying it
// clears the override so each mode keeps its own tuned default.
export const DEFAULT_ACCENT = "#2563eb";

export const ACCENT_STORAGE_KEY = "manga-reader-accent";

const HEX_RE = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i;

function parseHex(hex: string): { r: number; g: number; b: number } | null {
  const match = HEX_RE.exec(hex.trim());
  if (!match) return null;
  let h = match[1];
  if (h.length === 3) {
    h = h
      .split("")
      .map((c) => c + c)
      .join("");
  }
  const n = Number.parseInt(h, 16);
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}

export function isValidHexColor(value: string): boolean {
  return parseHex(value) !== null;
}

function normalizeHex(value: string): string {
  const match = HEX_RE.exec(value.trim());
  if (!match) return value;
  let h = match[1];
  if (h.length === 3) {
    h = h
      .split("")
      .map((c) => c + c)
      .join("");
  }
  return `#${h.toLowerCase()}`;
}

// WCAG relative luminance, used to pick readable text on top of the accent.
function relativeLuminance(hex: string): number {
  const rgb = parseHex(hex);
  if (!rgb) return 0;
  const channel = (c: number) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return (
    0.2126 * channel(rgb.r) +
    0.7152 * channel(rgb.g) +
    0.0722 * channel(rgb.b)
  );
}

export function accentContrast(hex: string): string {
  return relativeLuminance(hex) > 0.55 ? "#0b1220" : "#ffffff";
}

export function normalizeAccent(value: string): string {
  return normalizeHex(value);
}

// Mixes two hex colors in sRGB. Returns the first color unchanged if either
// input cannot be parsed.
export function mixHex(from: string, to: string, weight: number): string {
  const a = parseHex(from);
  const b = parseHex(to);
  if (!a || !b) return from;
  const t = Math.min(1, Math.max(0, weight));
  const mix = (x: number, y: number) => Math.round(x + (y - x) * t);
  const hex = (n: number) => n.toString(16).padStart(2, "0");
  return `#${hex(mix(a.r, b.r))}${hex(mix(a.g, b.g))}${hex(mix(a.b, b.b))}`;
}

// Which accent to actually apply for a given mode. Every accent — the built-in
// default, presets and custom colors — goes through the same rule: light mode
// uses the color as-is, dark mode uses a lightened variant so it stays legible
// on dark surfaces.
export function resolveAccent(
  accent: string | null,
  mode: "light" | "dark",
): string {
  const base = accent ?? DEFAULT_ACCENT;
  return mode === "dark" ? mixHex(base, "#ffffff", 0.3) : base;
}

export function isPresetAccent(accent: string | null): boolean {
  if (!accent) return false;
  const normalized = normalizeHex(accent);
  return ACCENT_PRESETS.some((preset) => preset.color === normalized);
}
