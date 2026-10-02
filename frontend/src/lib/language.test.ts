import { describe, expect, it } from "vitest";
import { languageCodeFromTags } from "./language";

describe("languageCodeFromTags", () => {
  it("maps known language tags to short codes", () => {
    expect(languageCodeFromTags(["language:chinese", "female:yuri"])).toBe("zh");
    expect(languageCodeFromTags(["language:japanese"])).toBe("jp");
    expect(languageCodeFromTags(["language:english"])).toBe("en");
  });

  it("is case-insensitive and trims", () => {
    expect(languageCodeFromTags(["language:English"])).toBe("en");
  });

  it("falls back to the raw name for unknown languages", () => {
    expect(languageCodeFromTags(["language:klingon"])).toBe("klingon");
  });

  it("returns null when there is no language tag", () => {
    expect(languageCodeFromTags(["female:yuri"])).toBeNull();
    expect(languageCodeFromTags([])).toBeNull();
    expect(languageCodeFromTags()).toBeNull();
    expect(languageCodeFromTags(null)).toBeNull();
  });
});
