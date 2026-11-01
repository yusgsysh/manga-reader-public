package app

import (
	"testing"
	"time"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// TestFormatPosted mirrors main's formatPosted cases: a date alone, a UTC
// datetime shown in the local zone, and the relative forms within the hour
// and earlier the same day.
func TestFormatPosted(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		posted string
		want   string
	}{
		{"2024-01-02", "2024/1/2"},
		{"2026-10-28 09:30", i18n.Tf("Today %s", "09:30")},
		{"2026-10-28 11:55", i18n.Tf("%d minutes ago", 5)},
		{"2026-08-29 10:00", "8/29 10:00"},
		{"2024-08-29 10:00", "2024/8/29 10:00"},
		{"", ""},
		{"not a date", "not a date"},
	}
	for _, c := range cases {
		if got := formatPosted(c.posted, now); got != c.want {
			t.Errorf("formatPosted(%q) = %q, want %q", c.posted, got, c.want)
		}
	}
}

// TestLanguageCode reads the first language tag as main's cards do.
func TestLanguageCode(t *testing.T) {
	tags := []apiTag{
		{Namespace: "female", Name: "yuri"},
		{Namespace: "language", Name: "chinese"},
		{Namespace: "language", Name: "japanese"},
	}
	if got := languageCode(tags); got != "zh" {
		t.Errorf("languageCode = %q, want zh", got)
	}
	if got := languageCode([]apiTag{{Namespace: "language", Name: "klingon"}}); got != "klingon" {
		t.Errorf("unknown language = %q, want its own name", got)
	}
	if got := languageCode(nil); got != "" {
		t.Errorf("languageCode(nil) = %q, want empty", got)
	}
}
