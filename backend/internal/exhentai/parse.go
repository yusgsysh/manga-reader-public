package exhentai

import (
	"regexp"
	"strconv"
	"strings"
)

var starsReg = regexp.MustCompile(`background-position:(-?\d+)px (-\d+)px`)

func ParseStars(stars string) float64 {
	matches := starsReg.FindStringSubmatch(stars)
	if len(matches) == 0 {
		return 0
	}
	x, _ := strconv.Atoi(matches[1])
	y, _ := strconv.Atoi(matches[2])

	rating := 5.0 - float64(-x/16)
	switch y {
	case -1:
	case -21:
		rating -= 0.5
	default:
		rating -= float64(y+1) / 20.0
	}
	return rating
}

func ParseGalleryURL(u string) (domain, gId, gToken string) {
	u = strings.TrimSuffix(u, "/")
	splits := strings.Split(u, "/")
	for i, s := range splits {
		if s == "g" && i > 0 && i+2 < len(splits) {
			return splits[i-1], splits[i+1], splits[i+2]
		}
	}
	return "", "", ""
}

func BuildCategoryFilter(categories []string) string {
	var cat uint
	for _, c := range categories {
		switch strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(c), "-", ""), "_", "")) {
		case "MISC":
			cat |= 1 << 0
		case "DOUJINSHI":
			cat |= 1 << 1
		case "MANGA":
			cat |= 1 << 2
		case "ARTISTCG":
			cat |= 1 << 3
		case "GAMECG":
			cat |= 1 << 4
		case "IMAGESET":
			cat |= 1 << 5
		case "COSPLAY":
			cat |= 1 << 6
		case "ASIANPORN":
			cat |= 1 << 7
		case "NONH":
			cat |= 1 << 8
		case "WESTERN":
			cat |= 1 << 9
		}
	}
	if cat == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(1023^cat), 10)
}
