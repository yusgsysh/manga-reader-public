package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// chineseLiteral finds a double-quoted string that starts with Chinese: the
// case this guard covers is a whole label (or a format string starting with
// one) handed to the UI. Mixed strings that start in English are handled by
// the translation table's own coverage test.
var chineseLiteral = regexp.MustCompile(`"([\p{Han}](?:[^"\\]|\\.)*)"`)

// TestNoHardcodedInterfaceText guards the translation: interface strings go
// through i18n.T (the English key), so a Chinese literal in the app's view
// code is either data or a string a translator can never see. The allowlist
// holds the deliberate ones: the category names the API reports (which are
// data, not UI copy) and the language names the settings row shows in it.
func TestNoHardcodedInterfaceText(t *testing.T) {
	allowed := map[string]bool{
		// Category labels come from ExHentai's category codes; the site's
		// names are part of the data, not the interface.
		"同人志": true, "漫画": true, "画集": true, "游戏 CG": true,
		"Manga": true, "Doujinshi": true,
		"图像集": true, "角色扮演": true, "亚种": true, "杂项": true,
		"西方": true, "非H": true, "其他": true, "亚洲": true, "图集": true,
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || strings.HasPrefix(f, "i18n") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for n, line := range strings.Split(string(src), "\n") {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "//") {
				continue
			}
			// A literal already wrapped in i18n.T is a Chinese translation
			// argument — none exist, but skip them anyway.
			for _, m := range chineseLiteral.FindAllStringSubmatch(line, -1) {
				s := m[1]
				if allowed[s] || strings.Contains(line, "i18n.T(") {
					continue
				}
				offenders = append(offenders, f+":"+itoa(n+1)+"  "+trim)
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("hard-coded interface text in %d places:\n%s",
			len(offenders), strings.Join(offenders, "\n"))
	}
}

// TestChineseCoversEveryKey checks the translation is complete in the other
// direction: every English key the app uses has a Chinese entry.
func TestChineseCoversEveryKey(t *testing.T) {
	// The keys are the i18n.T/Tf arguments in the view code.
	keyArg := regexp.MustCompile(`i18n\.T(?:f)?\("((?:[^"\\]|\\.)*)"`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	seen := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, _ := os.ReadFile(f)
		for _, m := range keyArg.FindAllSubmatch(src, -1) {
			k := string(m[1])
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	if len(keys) < 100 {
		t.Fatalf("only %d keys found; the scan is broken", len(keys))
	}
	if missing := i18n.Missing(keys); len(missing) > 0 {
		t.Fatalf("%d keys have no Chinese translation:\n%s",
			len(missing), strings.Join(missing, "\n"))
	}
}

// TestNoConcatenatedTranslations guards a mistake that reads fine in one
// language and is nonsense in the other: a translated fragment spliced onto
// a translated sentence (line += i18n.Sprintf(" · %d …")). Each message is
// a whole sentence, so its punctuation and word order are the translator's.
func TestNoConcatenatedTranslations(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, _ := os.ReadFile(f)
		for n, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			// A translated literal glued with + or appended with +=.
			if strings.Contains(line, "+= i18n.") ||
				regexp.MustCompile(`\+ i18n\.Tf?\(\s*"`).MatchString(line) {
				offenders = append(offenders, f+":"+itoa(n+1)+"  "+strings.TrimSpace(line))
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("translations concatenated instead of written whole:\n%s",
			strings.Join(offenders, "\n"))
	}
}
