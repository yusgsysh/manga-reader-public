package app

import (
	"reflect"
	"testing"
)

// newLayoutReader returns an app whose reader is set up for layout questions
// only: no gallery, just the page count and the settings under test.
func newLayoutReader(total int, s readerSettings) *App {
	a := New(NewClient("http://127.0.0.1:1"))
	for i := 0; i < total; i++ {
		a.reader.pages = append(a.reader.pages, "https://img.example/p"+itoa(i)+".jpg")
	}
	a.reader.settings = s
	return a
}

func TestPageIndexesForSingle(t *testing.T) {
	a := newLayoutReader(5, readerSettings{})
	for i := 0; i < 5; i++ {
		if got := a.pageIndexesFor(i, 5).indexes; !reflect.DeepEqual(got, []int{i}) {
			t.Fatalf("page %d alone = %v, want [%d]", i, got, i)
		}
		if step := a.pageStep(i); step != 1 {
			t.Fatalf("single step from %d = %d, want 1", i, step)
		}
	}
}

// A spread without a cover pairs 0-1, 2-3, and turns by two from either page;
// a pair asked for from its second page is the same pair.
func TestPageIndexesForSpreadWithoutCover(t *testing.T) {
	a := newLayoutReader(6, readerSettings{double: true})
	want := map[int][]int{
		0: {0, 1}, 1: {0, 1},
		2: {2, 3}, 3: {2, 3},
		4: {4, 5}, 5: {4, 5},
	}
	for i, w := range want {
		if got := a.pageIndexesFor(i, 6).indexes; !reflect.DeepEqual(got, w) {
			t.Fatalf("spread at %d = %v, want %v", i, got, w)
		}
	}
	if step := a.pageStep(0); step != 2 {
		t.Fatalf("spread step from 0 = %d, want 2", step)
	}
}

// Right-to-left reverses the visual order: the later page on the left.
func TestPageIndexesForSpreadRTL(t *testing.T) {
	a := newLayoutReader(6, readerSettings{double: true, rtl: true})
	if got := a.pageIndexesFor(2, 6).indexes; !reflect.DeepEqual(got, []int{3, 2}) {
		t.Fatalf("rtl spread at 2 = %v, want [3 2]", got)
	}
	if got := a.pageIndexesFor(3, 6).indexes; !reflect.DeepEqual(got, []int{3, 2}) {
		t.Fatalf("rtl spread at 3 = %v, want [3 2]", got)
	}
}

// With a cover, the first page stands alone and the spreads start at the
// second page: 1-2, 3-4……
func TestPageIndexesForSpreadWithCover(t *testing.T) {
	a := newLayoutReader(6, readerSettings{double: true, hasCover: true})
	want := map[int][]int{
		0: {0},
		1: {1, 2}, 2: {1, 2},
		3: {3, 4}, 4: {3, 4},
		5: {5},
	}
	for i, w := range want {
		if got := a.pageIndexesFor(i, 6).indexes; !reflect.DeepEqual(got, w) {
			t.Fatalf("spread at %d = %v, want %v", i, got, w)
		}
	}
	// The cover turns by one, the spreads by two.
	if step := a.pageStep(0); step != 1 {
		t.Fatalf("step from the cover = %d, want 1", step)
	}
	if step := a.pageStep(1); step != 2 {
		t.Fatalf("step from a spread = %d, want 2", step)
	}
}

// The last page of an odd gallery is alone in its spread.
func TestPageIndexesForOddLastPage(t *testing.T) {
	a := newLayoutReader(5, readerSettings{double: true})
	if got := a.pageIndexesFor(4, 5).indexes; !reflect.DeepEqual(got, []int{4}) {
		t.Fatalf("last page of 5 = %v, want [4]", got)
	}
}

// A narrow window shows one page at a time whatever the setting says, as
// comimi does on a phone-width viewport.
func TestNarrowWindowIgnoresSpread(t *testing.T) {
	a := newLayoutReader(6, readerSettings{double: true})
	a.reader.narrow = true
	if got := a.pageIndexesFor(2, 6).indexes; !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("narrow window at 2 = %v, want [2]", got)
	}
	if step := a.pageStep(2); step != 1 {
		t.Fatalf("narrow step = %d, want 1", step)
	}
}

// A turn lands where the gesture that brings it comes from: with a cover the
// cover turns to the first spread, spreads turn two at a time, and either end
// has no neighbour to turn to.
func TestAdjacentGroup(t *testing.T) {
	a := newLayoutReader(6, readerSettings{double: true, hasCover: true})
	a.reader.page = 0
	if got := a.adjacentGroup(groupNext).indexes; !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("next from the cover = %v, want [1 2]", got)
	}
	if got := a.adjacentGroup(groupPrevious).indexes; len(got) != 0 {
		t.Fatalf("previous from the cover = %v, want none", got)
	}

	a.reader.page = 3
	if got := a.adjacentGroup(groupNext).indexes; !reflect.DeepEqual(got, []int{5}) {
		t.Fatalf("next from 3-4 = %v, want [5]", got)
	}
	if got := a.adjacentGroup(groupPrevious).indexes; !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("previous from 3-4 = %v, want [1 2]", got)
	}

	a.reader.page = 5
	if got := a.adjacentGroup(groupNext).indexes; len(got) != 0 {
		t.Fatalf("next from the last page = %v, want none", got)
	}
}

// The stage lays out one group either side of the shown one, each a screen
// away: for left-to-right the next group is to the right, for right-to-left
// to the left.
func TestPageGroupsToLayOut(t *testing.T) {
	// Right to left is the reader's default: the next group waits to the
	// left, where the gesture that brings it comes from.
	a := newLayoutReader(9, readerSettings{rtl: true})
	a.reader.page = 4
	specs := a.pageGroupsToLayOut()
	if len(specs) != 3 {
		t.Fatalf("groups = %d, want 3", len(specs))
	}
	wantOffsets := map[pageGroupPlacement]int{
		groupPrevious: 1, groupCurrent: 0, groupNext: -1,
	}
	for _, spec := range specs {
		if got := a.groupOffsetX(spec); got != wantOffsets[spec.placement] {
			t.Fatalf("%v offset = %d, want %d", spec.placement, got, wantOffsets[spec.placement])
		}
		if spec.placement == groupCurrent && !reflect.DeepEqual(spec.pages.indexes, []int{4}) {
			t.Fatalf("current group = %v, want [4]", spec.pages.indexes)
		}
	}

	// Left to right turns the other way round.
	a.reader.settings.rtl = false
	for _, spec := range a.pageGroupsToLayOut() {
		if spec.placement == groupNext && a.groupOffsetX(spec) != 1 {
			t.Fatalf("ltr next offset = %d, want 1", a.groupOffsetX(spec))
		}
		if spec.placement == groupPrevious && a.groupOffsetX(spec) != -1 {
			t.Fatalf("ltr previous offset = %d, want -1", a.groupOffsetX(spec))
		}
	}
}

// At either end the stage lays out only the groups that exist.
func TestPageGroupsToLayOutAtTheEnds(t *testing.T) {
	a := newLayoutReader(3, readerSettings{})
	a.reader.page = 0
	if specs := a.pageGroupsToLayOut(); len(specs) != 2 {
		t.Fatalf("groups at the first page = %d, want 2", len(specs))
	}
	a.reader.page = 2
	if specs := a.pageGroupsToLayOut(); len(specs) != 2 {
		t.Fatalf("groups at the last page = %d, want 2", len(specs))
	}
}

func TestPageGroupText(t *testing.T) {
	cases := []struct {
		g    pageGroup
		want string
	}{
		{pageGroup{indexes: []int{0}}, "1"},
		{pageGroup{indexes: []int{2, 3}}, "3 - 4"},
		{pageGroup{indexes: []int{3, 2}}, "3 - 4"},
		{pageGroup{}, ""},
	}
	for _, c := range cases {
		if got := pageGroupText(c.g); got != c.want {
			t.Fatalf("text of %v = %q, want %q", c.g.indexes, got, c.want)
		}
	}
}
