package i18n

import (
	"strings"
	"testing"

	"golang.org/x/text/language"
)

func TestTranslationFallsBackToTheKey(t *testing.T) {
	l := &Localizer{}
	l.Set(language.Chinese)
	if got := l.T("Browse"); got != "浏览" {
		t.Fatalf("zh Browse = %q", got)
	}
	l.Set(language.English)
	if got := l.T("Browse"); got != "Browse" {
		t.Fatalf("en Browse = %q, want the key", got)
	}
	// A key with no translation says itself in any language, never "".
	if got := l.T("A message nobody translated"); got != "A message nobody translated" {
		t.Fatalf("missing key = %q", got)
	}
}

func TestUnsupportedLanguageFallsBackToEnglish(t *testing.T) {
	l := &Localizer{}
	l.Set(language.Japanese)
	if got := l.T("Home"); got != "Home" {
		t.Fatalf("ja Home = %q, want English fallback", got)
	}
}

func TestFormattedMessagesKeepTheirVerbs(t *testing.T) {
	SetLanguage(language.Chinese)
	defer SetLanguage(language.Chinese)
	got := Tf("Continue (page %d)", 3)
	if got != "继续阅读（第 3 页）" {
		t.Fatalf("continue = %q", got)
	}
	got = Tf("%s · %d pages", "漫画", 12)
	if got != "漫画 · 12 页" {
		t.Fatalf("pages = %q", got)
	}
}

// TestEveryFormatTranslationKeepsItsVerbs guards the one mistake a
// translation can make silently: dropping or adding a % verb makes the
// formatted string print %!d(MISSING) or extra text at run time.
func TestEveryFormatTranslationKeepsItsVerbs(t *testing.T) {
	for en, cn := range zh {
		if strings.Count(en, "%") != strings.Count(cn, "%") {
			t.Errorf("%q → %q: verb count differs", en, cn)
		}
	}
}

func TestNamesAreLocalised(t *testing.T) {
	l := &Localizer{}
	l.Set(language.Chinese)
	if got := l.Names(); len(got) != 2 || got[0] != "简体中文" || got[1] != "English" {
		t.Fatalf("zh names = %v", got)
	}
	l.Set(language.English)
	if got := l.Names(); len(got) != 2 || got[0] != "Simplified Chinese" {
		t.Fatalf("en names = %v", got)
	}
}

func TestNormaliseSavedTags(t *testing.T) {
	cases := map[string]language.Tag{
		"":          language.English,
		"zh-CN":     language.Chinese,
		"zh-Hans":   language.Chinese,
		"zh-TW":     language.Chinese,
		"en":        language.English,
		"en-GB":     language.English,
		"not a tag": language.English,
		"ja":        language.English,
	}
	for in, want := range cases {
		if got := Normalise(in); got != want {
			t.Errorf("Normalise(%q) = %v, want %v", in, got, want)
		}
	}
}
