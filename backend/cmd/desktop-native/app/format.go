package app

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"manga-reader/cmd/desktop-native/app/i18n"
)

var (
	postedDateOnly = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	postedDateTime = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})`)
)

// formatPosted renders the API's posted string as main's lib/time.ts does:
// a date alone as "2024/1/1", a UTC datetime converted to the local zone and
// shown relative to now — "10分钟前" within the hour, "今天10:00" earlier
// today, "8/29 10:00" earlier this year, the full date otherwise. Anything
// else passes through.
func formatPosted(posted string, now time.Time) string {
	trimmed := strings.TrimSpace(posted)
	if trimmed == "" {
		return ""
	}
	if postedDateOnly.MatchString(trimmed) {
		var year, month, day int
		fmt.Sscanf(trimmed, "%d-%d-%d", &year, &month, &day)
		return fmt.Sprintf("%d/%d/%d", year, month, day)
	}
	m := postedDateTime.FindStringSubmatch(trimmed)
	if m == nil {
		return trimmed
	}
	part := func(i int) int {
		n, _ := strconv.Atoi(m[i])
		return n
	}
	at := time.Date(
		part(1), time.Month(part(2)), part(3), part(4), part(5), 0, 0, time.UTC,
	).In(now.Location())

	if diff := now.Sub(at); diff >= 0 && diff < time.Hour {
		minutes := int(diff / time.Minute)
		if minutes < 1 {
			return i18n.T("Just now")
		}
		return i18n.Tf("%d minutes ago", minutes)
	}
	clock := fmt.Sprintf("%02d:%02d", at.Hour(), at.Minute())
	if at.Year() == now.Year() && at.Month() == now.Month() && at.Day() == now.Day() {
		return i18n.Tf("Today %s", clock)
	}
	if at.Year() == now.Year() {
		return fmt.Sprintf("%d/%d %s", at.Month(), at.Day(), clock)
	}
	return fmt.Sprintf("%d/%d/%d %s", at.Year(), at.Month(), at.Day(), clock)
}

// postedText is formatPosted for the current time.
func postedText(posted string) string {
	return formatPosted(posted, time.Now())
}
