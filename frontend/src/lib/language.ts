// Maps ExHentai's `language:*` tag names to short codes for compact display.
const LANGUAGE_CODES: Record<string, string> = {
  chinese: "zh",
  japanese: "jp",
  english: "en",
  korean: "ko",
  spanish: "es",
  french: "fr",
  german: "de",
  italian: "it",
  portuguese: "pt",
  russian: "ru",
  thai: "th",
  vietnamese: "vi",
  indonesian: "id",
  dutch: "nl",
  polish: "pl",
  hungarian: "hu",
  arabic: "ar",
  czech: "cs",
  greek: "el",
  hebrew: "he",
  hindi: "hi",
  ukrainian: "uk",
  romanian: "ro",
  turkish: "tr",
  catalan: "ca",
  croatian: "hr",
};

// Returns the short code (e.g. "zh", "jp") of the first `language:*` tag, or
// null when the gallery has no language tag.
export function languageCodeFromTags(tags?: string[] | null): string | null {
  if (!tags) return null;
  for (const raw of tags) {
    const index = raw.indexOf(":");
    if (index <= 0) continue;
    if (raw.slice(0, index) !== "language") continue;
    const name = raw.slice(index + 1).trim().toLowerCase();
    if (!name) continue;
    return LANGUAGE_CODES[name] ?? name;
  }
  return null;
}
