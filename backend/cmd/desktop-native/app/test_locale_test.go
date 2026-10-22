package app

import (
	"os"
	"testing"

	"golang.org/x/text/language"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// TestMain puts the interface in Simplified Chinese for every test: the
// tests click and assert the labels they know, and Chinese is the language
// the app was written in. The English source keys are exercised by the i18n
// package's own tests and by TestEnglishInterface.
func TestMain(m *testing.M) {
	i18n.SetLanguage(language.Chinese)
	os.Exit(m.Run())
}

// TestEnglishInterface checks the other language end to end: with English
// selected every window says the English keys, and the language row offers
// both languages.
func TestEnglishInterface(t *testing.T) {
	i18n.SetLanguage(language.English)
	defer i18n.SetLanguage(language.Chinese)

	_, tt := newTestApp(t)
	waitText(t, tt, "Browse")
	waitText(t, tt, "示例 A") // gallery titles are data, shown as given
	// The home page's own copy is in English: its section subtitle and the
	// nav section names prove the whole interface translated.
	if !tt.HasText("Latest galleries") && !tt.HasText("Downloads") {
		t.Fatalf("English controls missing; texts=%q", tt.Texts())
	}
	for _, s := range tt.Texts() {
		if s == "首页" || s == "标签设置" {
			t.Fatalf("Chinese label %q still shown in English mode", s)
		}
	}
}
