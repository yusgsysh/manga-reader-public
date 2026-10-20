package app

import (
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
	Pages    int
	Rating   float64
	Tags     []apiTag
	CoverURL string
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
