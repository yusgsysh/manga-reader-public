// Package i18n holds the interface strings in the languages the app is
// translated to. English is the source: every call site passes the English
// text as its key, and a missing translation falls back to it, so the app
// can never show an empty label.
//
// Chinese is the first translation (the app's original language). The
// settings page chooses it; mygo.App.SetLocale applies it and every window
// redraws in it. Only the Simplified Chinese UI is translated today, so a
// system asking for another language gets English, which is at least
// readable — and the language row says which languages exist.
package i18n

import (
	"strings"
	"sync"

	"golang.org/x/text/language"
)

// The languages the interface is translated to, in the order the settings
// page lists them. The base tag names the message function each uses.
var Supported = []language.Tag{
	language.Chinese,
	language.English,
}

// Names are the languages' names in the interface's own language, for the
// settings row: 简体中文/English in Chinese, Simplified Chinese/English in
// English.
func (l *Localizer) Names() []string {
	names := make([]string, len(Supported))
	for i, tag := range Supported {
		names[i] = l.T(countryName(tag))
	}
	return names
}

// countryName is the English key of a language's name.
func countryName(tag language.Tag) string {
	if base, _ := tag.Base(); base.String() == "zh" {
		return "Simplified Chinese"
	}
	return "English"
}

// Localizer answers the app's strings in one language. It is set once at
// startup (SetLanguage) and read by every frame; before that it says English.
type Localizer struct {
	mu   sync.RWMutex
	tag  language.Tag
	lang string // base language, "zh" or "en"
}

// Default is the localizer the app uses.
var Default = &Localizer{tag: language.English, lang: "en"}

// SetLanguage points the default localizer at tag. An unsupported language
// falls back to English.
func SetLanguage(tag language.Tag) {
	Default.Set(tag)
}

// Set changes the language the localizer says its strings in.
func (l *Localizer) Set(tag language.Tag) {
	base, _ := tag.Base()
	lang := base.String()
	switch lang {
	case "zh", "en":
	default:
		lang = "en"
	}
	l.mu.Lock()
	l.tag, l.lang = tag, lang
	l.mu.Unlock()
}

// Language is the tag the localizer currently uses.
func (l *Localizer) Language() language.Tag {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.tag
}

// T says the message key (an English sentence) in the localizer's language,
// falling back to the key itself when it has no translation.
func (l *Localizer) T(key string) string {
	l.mu.RLock()
	lang := l.lang
	l.mu.RUnlock()
	if lang == "zh" {
		if s, ok := zh[key]; ok {
			return s
		}
	}
	return key
}

// T is T of the default localizer.
func T(key string) string { return Default.T(key) }

// Tf is T for a key holding one %s or %d, substituted by values. Kept to a
// single verb on purpose: translators see the format in the English key.
func Tf(key string, values ...any) string {
	return Sprintf(T(key), values...)
}

// Sprintf is fmt.Sprintf, isolated so call sites read as one string call.
func Sprintf(format string, values ...any) string {
	return sprintf(format, values...)
}

// Has reports whether key has a translation in lang, for tests that guard
// coverage.
func Has(lang, key string) bool {
	if lang != "zh" {
		return true
	}
	_, ok := zh[key]
	return ok
}

// Missing lists the keys of one language with no translation yet.
func Missing(keys []string) []string {
	var out []string
	for _, k := range keys {
		if _, ok := zh[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}

// Normalise maps a saved language tag ("zh-CN", "zh-Hans", "en", "") to the
// supported one it means: any Chinese to Simplified Chinese, anything else
// unrecognised to English.
func Normalise(saved string) language.Tag {
	saved = strings.TrimSpace(saved)
	if saved == "" {
		return language.English
	}
	tag, err := language.Parse(saved)
	if err != nil {
		return language.English
	}
	if base, _ := tag.Base(); base.String() == "zh" {
		return language.Chinese
	}
	return language.English
}
