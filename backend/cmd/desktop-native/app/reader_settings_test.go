package app

import (
	"fmt"
	"testing"
	"time"
)

// TestReaderSpreadSettingLandsOnThePair covers the settings sheet's page-mode
// checkbox: turning spreads on from the second page of a pair keeps that pair
// together instead of pairing the page with the one before it.
func TestReaderSpreadSettingLandsOnThePair(t *testing.T) {
	a, tt := newTestApp(t)
	a.router.Push("/reader/1/tok1")
	navSettle(tt)
	waitText(t, tt, "第 1 / 3 页")

	a.update(func() { a.goToPage(&a.reader, 2) })
	tt.Frame()
	showReaderPanel(t, a, tt, panelSettings)
	if err := tt.Click("双页显示"); err != nil {
		t.Fatalf("click the spread checkbox: %v", err)
	}
	if !a.reader.settings.double {
		t.Fatal("the checkbox did not turn spreads on")
	}
	if a.reader.page != 1 {
		t.Fatalf("page = %d, want 1, the first page of the pair", a.reader.page)
	}
	if got := a.readerSettings; !got.double {
		t.Fatalf("remembered settings = %+v, want spreads", got)
	}
}

// TestReaderPageListOpensWhereItIs covers the page list opening on the page
// being read: a long gallery would otherwise start at page one every time.
func TestReaderPageListOpensWhereItIs(t *testing.T) {
	a, tt := newTestApp(t)
	a.router.Push("/reader/1/tok1")
	navSettle(tt)
	waitText(t, tt, "第 1 / 3 页")

	a.update(func() {
		r := &a.reader
		r.pages = make([]string, 60)
		for i := range r.pages {
			r.pages[i] = fmt.Sprintf("https://img.example/p%d.jpg", i)
		}
		r.total, r.partial, r.complete, r.loaded = 60, 60, true, true
		a.goToPage(r, 40)
		a.openPanel(r, panelPages)
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if a.reader.pagesScroll.Y > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if a.reader.pagesScroll.Y <= 0 {
		t.Fatalf("page list stayed at the top: y = %v", a.reader.pagesScroll.Y)
	}
}
