package app

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
	Tags     []string
	CoverURL string
	Reading  *ReadingProgress
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
		return "同人志"
	case "manga":
		return "漫画"
	case "artistcg":
		return "画师 CG"
	case "gamecg":
		return "游戏 CG"
	case "western":
		return "欧美"
	case "image-set":
		return "图集"
	case "cosplay":
		return "Cosplay"
	case "asianporn":
		return "亚洲"
	case "non-h":
		return "非 H"
	case "misc":
		return "杂项"
	case "other":
		return "其他"
	default:
		return code
	}
}
