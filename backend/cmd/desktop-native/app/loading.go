package app

import (
	"time"

	"github.com/egoist/mygo/plugins/loading"
	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// The loading animations come from mygo's loading plugin, which draws
// loading.dev's indicators as native elements. This file keeps every place
// that waits on the network to one look: a large indicator on a page that is
// still empty, a small one beside a label while something is refreshed.

// The two sizes the interface uses. inlineSpinnerSize sits with 11–12 pt
// text; pageSpinnerSize heads a full-page "Loading…" state.
const (
	inlineSpinnerSize float32 = 16
	pageSpinnerSize   float32 = 32
)

// inlineLoading is the small indicator that turns beside a line of text
// while work is in progress. label names what is loading, for assistive
// technology; empty falls back to "Loading…".
func inlineLoading(c *ui.Context, label string) {
	if label == "" {
		label = i18n.T("Loading…")
	}
	loading.BouncingDots(c, loading.Options{
		Size:     inlineSpinnerSize,
		Color:    c.Theme().Accent,
		Duration: 900 * time.Millisecond,
	}).Label(label)
}

// pageLoading is the larger indicator that marks a page still fetching its
// first content. label names what is loading; empty falls back to "Loading…".
func pageLoading(c *ui.Context, label string) {
	if label == "" {
		label = i18n.T("Loading…")
	}
	loading.Ring(c, loading.Options{
		Size:   pageSpinnerSize,
		Color:  c.Theme().Accent,
		Easing: loading.EaseInOut,
	}).Label(label)
}
