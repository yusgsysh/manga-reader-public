package app

import (
	"testing"
)

// The reader's top row is the title pill on the left and the way back to the
// gallery on the right. The two are cards of their own: the title must stay on
// one line between them, and the back button must keep its label, or at the
// narrowest window its arrow alone, without either pushing the other around.
func TestReaderTopRowKeepsTitleAndBackApart(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	if err := tt.Click("示例 A"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")
	if err := tt.Click("开始阅读"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitReaderPage(t, tt, a, 0)

	// A long title is the case that used to squeeze the row: it should be
	// cut inside the pill rather than wrap under the back button.
	const longTitle = "这是一个很长的画廊标题，用来检查阅读器顶部标题与返回按钮是否仍然对齐"
	a.update(func() { a.reader.title = longTitle })

	for _, tc := range []struct {
		name      string
		width     int
		wantLabel bool
	}{
		{"wide", 1280, true},
		{"label", 640, true},
		{"compact", 480, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tt.SetSize(tc.width, 700)
			showReaderOverlay(t, a, tt)

			back, ok := tt.Find("返回画廊")
			if !ok {
				t.Fatalf("the back-to-gallery button is missing; texts=%q", tt.Texts())
			}
			menu, ok := tt.Find("阅读器菜单")
			if !ok {
				t.Fatalf("the reader menu button is missing; texts=%q", tt.Texts())
			}
			title, ok := tt.Find(longTitle)
			if !ok {
				t.Fatalf("the gallery title is missing; texts=%q", tt.Texts())
			}

			// The button has a card of its own, flush with the window's
			// inset, and its content is centred on the title pill's.
			inset := readerInset(float32(tc.width))
			if got := back.X + back.W; absFloat(got-(float32(tc.width)-inset)) > 0.5 {
				t.Errorf("back card's right edge = %v, want %v", got, float32(tc.width)-inset)
			}
			if got, want := back.Y+back.H/2, menu.Y+menu.H/2; absFloat(got-want) > 1 {
				t.Errorf("back card centre y = %v, want it level with the menu button at %v", got, want)
			}

			// The title stays one line and is centred with the icons; a
			// stretched text box would sit its glyphs above them.
			if title.H > 24 {
				t.Errorf("title box height = %v, want a single centred line", title.H)
			}
			if got, want := title.Y+title.H/2, menu.Y+menu.H/2; absFloat(got-want) > 1 {
				t.Errorf("title centre y = %v, want it level with the menu button at %v", got, want)
			}
			if got := title.X + title.W; got > back.X {
				t.Errorf("title ends at x = %v, past the back card at %v", got, back.X)
			}

			// The label shows while the window leaves it room, and the
			// compact card is the arrow alone.
			if tc.wantLabel && back.W <= readerBackSize {
				t.Errorf("back card width = %v, want room for its label", back.W)
			}
			if !tc.wantLabel && absFloat(back.W-readerBackSize) > 0.5 {
				t.Errorf("compact back card width = %v, want %v", back.W, readerBackSize)
			}
		})
	}
}

// The compact back card is the way out of the reader too, not just a button
// that looks the part.
func TestReaderCompactBackLeavesForTheGallery(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	if err := tt.Click("示例 A"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")
	if err := tt.Click("开始阅读"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitReaderPage(t, tt, a, 0)

	tt.SetSize(480, 700)
	showReaderOverlay(t, a, tt)
	back, ok := tt.Find("返回画廊")
	if !ok {
		t.Fatalf("the compact back button is missing; texts=%q", tt.Texts())
	}
	if absFloat(back.W-readerBackSize) > 0.5 {
		t.Fatalf("compact back card width = %v, want %v", back.W, readerBackSize)
	}
	if err := tt.Click("返回画廊"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	if got := a.router.Path(); got != "/gallery/1/tok1" {
		t.Fatalf("path after the compact back = %q, want the gallery", got)
	}
}
