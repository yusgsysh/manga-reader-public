package app

import (
	"strings"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// Gallery is one gallery as the native UI shows it: the fields of the list and
// detail API responses that the browsing and reading pages use.
type Gallery struct {
	ID       int64
	Token    string
	Title    string
	TitleJPN string
	Category string
	// CategoryCode is the API's category code (doujinshi, manga, …), kept
	// for the chip colour; Category is its translated label.
	CategoryCode string
	Pages        int
	Rating       float64
	Tags         []apiTag
	CoverURL     string
	// Posted is the API's posting time; the card shows it under its meta
	// line, as main's GalleryCard does.
	Posted string
	// Language is the short code of the gallery's language:* tag ("zh", "jp",
	// …), empty when it has none. Main derives it once per card; the list
	// derives it once per item.
	Language string
	Reading  *ReadingProgress
}

// NextPage is the page a "continue reading" button should open: the saved
// page, or 0 when the gallery has no progress or was finished.
func (g Gallery) NextPage() int {
	if g.Reading == nil || g.Reading.Completed {
		return 0
	}
	return g.Reading.CurrentPage
}

// ReadingProgress is the part of a gallery's reading progress the lists show.
type ReadingProgress struct {
	CurrentPage int
	Progress    float64
	Completed   bool
	// UpdatedAt is the API's updated_at, kept so a gallery reached through
	// one list can be matched with the same gallery in another (see
	// rememberReading).
	UpdatedAt string
}

// categoryLabel turns the API's category code into the label the UI shows.
func categoryLabel(code string) string {
	switch code {
	case "doujinshi":
		return i18n.T("Doujinshi")
	case "manga":
		return i18n.T("Manga")
	case "artistcg":
		return i18n.T("Artist CG")
	case "gamecg":
		return i18n.T("Game CG")
	case "western":
		return i18n.T("Western")
	case "image-set":
		return i18n.T("Image set")
	case "cosplay":
		return i18n.T("Cosplay")
	case "asianporn":
		return i18n.T("Asian porn")
	case "non-h":
		return i18n.T("Non-H")
	case "misc":
		return i18n.T("Misc")
	case "other":
		return i18n.T("Other")
	default:
		return code
	}
}

// languageCodes maps ExHentai's language:* tag names to the short codes the
// cards show, as main's lib/language.ts does.
var languageCodes = map[string]string{
	"chinese": "zh", "japanese": "jp", "english": "en", "korean": "ko",
	"spanish": "es", "french": "fr", "german": "de", "italian": "it",
	"portuguese": "pt", "russian": "ru", "thai": "th", "vietnamese": "vi",
	"indonesian": "id", "dutch": "nl", "polish": "pl", "hungarian": "hu",
	"arabic": "ar", "czech": "cs", "greek": "el", "hebrew": "he",
	"hindi": "hi", "ukrainian": "uk", "romanian": "ro", "turkish": "tr",
	"catalan": "ca", "croatian": "hr",
}

// languageCode is the short code of the first language:* tag, empty when the
// gallery has none. An unknown language keeps its own name, as main does.
func languageCode(tags []apiTag) string {
	for _, tag := range tags {
		if tag.Namespace != "language" {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(tag.Name))
		if name == "" {
			continue
		}
		if code, ok := languageCodes[name]; ok {
			return code
		}
		return name
	}
	return ""
}
