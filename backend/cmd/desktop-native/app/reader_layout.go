package app

import "strconv"

// Page layout for the reader, after comimi's page-layout.ts and the parts of
// its defaults.ts that decide what a turn shows (getPageStep,
// calculateVisiblePageIndexes). The rules a reader expects of a manga are all
// here, away from drawing:
//
//   - a single page shows one page and turns by one;
//   - a spread shows a pair, and which pages pair up depends on whether the
//     first page is a cover standing alone (hasCover);
//   - right-to-left reverses the visual order of a pair, so the later page
//     sits on the left;
//   - a turn anchors on the first or last page of the spread it shows, so a
//     spread never repeats or skips a page.

// readerPreloadGroups is how many page groups on either side of the shown one
// are laid out off screen (comimi's PRELOAD_PAGE_COUNT). One group back and
// one ahead makes a step in either direction instant.
const readerPreloadGroups = 1

// readerPreloadImages is how many pages on either side of the shown group have
// their pictures fetched ahead of time (comimi's PRELOAD_IMAGE_COUNT).
const readerPreloadImages = 4

// readerSpreadMinWidth is the window width under which a spread opens as
// single pages, as comimi does on a phone-width viewport: two pages of a
// narrow window would be unreadably small.
const readerSpreadMinWidth float32 = 900

// pageGroup is a set of pages shown together: one page, or the pair of a
// spread, in the visual order they are drawn (right-to-left reverses it).
type pageGroup struct {
	// indexes are the pages shown, in visual order, left to right.
	indexes []int
}

// itoa is strconv.Itoa under a name the reader uses for page labels.
func itoa(n int) string { return strconv.Itoa(n) }

// maxInt is the larger of a and b.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// placement says where a group sits relative to the shown one.
type pageGroupPlacement int

const (
	groupCurrent pageGroupPlacement = iota
	groupPrevious
	groupNext
)

// pageGroupSpec is a group the stage lays out: its pages, where it sits, and
// how many screen widths away.
type pageGroupSpec struct {
	pages     pageGroup
	placement pageGroupPlacement
	offset    int
}

// pageIndexesFor returns the pages shown together when the reader is on
// pageIndex, in visual order (comimi's getPageIndexesForPageIndex).
func (a *App) pageIndexesFor(pageIndex, total int) pageGroup {
	s := a.reader.settings
	if total <= 0 {
		return pageGroup{}
	}
	if pageIndex < 0 {
		pageIndex = 0
	}
	if pageIndex >= total {
		pageIndex = total - 1
	}
	// A single page, a narrow window, or the cover standing alone.
	if !s.spread() || a.reader.narrow || (s.hasCover && pageIndex == 0) {
		return pageGroup{indexes: []int{pageIndex}}
	}
	partner := pageIndex + 1
	if s.hasCover {
		// With a cover, odd pages start a pair (1-2, 3-4…).
		if pageIndex%2 == 0 {
			partner = pageIndex - 1
		}
	} else if pageIndex%2 == 1 {
		// Without one, even pages start a pair (0-1, 2-3…).
		partner = pageIndex - 1
	}
	indexes := []int{}
	if partner >= 0 && partner < total {
		indexes = append(indexes, partner)
	}
	indexes = append(indexes, pageIndex)
	// Ascending, then reversed for right-to-left: the later page on the left.
	if indexes[0] > indexes[len(indexes)-1] {
		indexes[0], indexes[len(indexes)-1] = indexes[len(indexes)-1], indexes[0]
	}
	if s.rtl {
		for i, j := 0, len(indexes)-1; i < j; i, j = i+1, j-1 {
			indexes[i], indexes[j] = indexes[j], indexes[i]
		}
	}
	return pageGroup{indexes: indexes}
}

// pageStep is how far a turn moves from pageIndex: one page in single mode,
// and at the cover where it stands alone (comimi's getPageStep).
func (a *App) pageStep(pageIndex int) int {
	s := a.reader.settings
	if !s.spread() || a.reader.narrow {
		return 1
	}
	if s.hasCover && pageIndex == 0 {
		return 1
	}
	return 2
}

// shownGroup is the pages the reader is showing now.
func (a *App) shownGroup() pageGroup {
	r := &a.reader
	return a.pageIndexesFor(r.page, len(r.pages))
}

// adjacentGroup is the group a turn in direction lands on, empty at either end
// (comimi's getAdjacentPageIndexes).
func (a *App) adjacentGroup(direction pageGroupPlacement) pageGroup {
	r := &a.reader
	total := len(r.pages)
	shown := a.shownGroup()
	if len(shown.indexes) == 0 {
		return pageGroup{}
	}
	// The anchor is the first or last page the group covers, not its first
	// or last drawn page: right-to-left draws a pair reversed, so the drawn
	// order says nothing about which way the pages go.
	anchor := 0
	if direction == groupNext {
		anchor = pageGroupLast(shown) + 1
	} else {
		anchor = pageGroupFirst(shown) - 1
	}
	if anchor < 0 || anchor >= total {
		return pageGroup{}
	}
	return a.pageIndexesFor(anchor, total)
}

// pageGroupsToLayOut is the shown group and its neighbours, nearest first, as
// the stage lays them out (comimi's getPreloadedPageGroups).
func (a *App) pageGroupsToLayOut() []pageGroupSpec {
	groups := make([]pageGroupSpec, 0, 2*readerPreloadGroups+1)
	// Farthest previous first and nearest next last, the order the stage
	// draws them in over one another, with the shown group in between.
	for distance := readerPreloadGroups; distance >= 1; distance-- {
		if g := a.groupAtDistance(groupPrevious, distance); len(g.indexes) > 0 {
			groups = append(groups, pageGroupSpec{pages: g, placement: groupPrevious, offset: -distance})
		}
	}
	groups = append(groups, pageGroupSpec{pages: a.shownGroup(), placement: groupCurrent})
	for distance := 1; distance <= readerPreloadGroups; distance++ {
		if g := a.groupAtDistance(groupNext, distance); len(g.indexes) > 0 {
			groups = append(groups, pageGroupSpec{pages: g, placement: groupNext, offset: distance})
		}
	}
	return groups
}

// groupAtDistance is the group distance turns away in direction, empty if the
// gallery ends before it.
func (a *App) groupAtDistance(direction pageGroupPlacement, distance int) pageGroup {
	g := a.shownGroup()
	total := len(a.reader.pages)
	for step := 0; step < distance; step++ {
		if len(g.indexes) == 0 {
			return pageGroup{}
		}
		anchor := 0
		if direction == groupNext {
			anchor = pageGroupLast(g) + 1
		} else {
			anchor = pageGroupFirst(g) - 1
		}
		if anchor < 0 || anchor >= total {
			return pageGroup{}
		}
		g = a.pageIndexesFor(anchor, total)
	}
	return g
}

// groupOffsetX is where a group is laid out along the reader's width, in
// screens from the shown one: the pages that turn in from the end of the
// reading direction (the right in a left-to-right read, the left in a
// right-to-left one) sit there, so a swipe that brings them in starts over
// them.
func (a *App) groupOffsetX(spec pageGroupSpec) int {
	if spec.placement == groupCurrent {
		return 0
	}
	// spec.offset is the distance as a sign: negative before, positive after.
	after := spec.offset > 0
	if a.reader.settings.rtl {
		after = !after
	}
	if after {
		return 1
	}
	return -1
}

// pageGroupText is the label the toolbar and the previews show for a group: one
// number, or the range a spread covers, 1-based as pages are counted.
func pageGroupText(g pageGroup) string {
	if len(g.indexes) == 0 {
		return ""
	}
	lo, hi := g.indexes[0], g.indexes[0]
	for _, i := range g.indexes[1:] {
		if i < lo {
			lo = i
		}
		if i > hi {
			hi = i
		}
	}
	if lo == hi {
		return itoa(lo + 1)
	}
	return itoa(lo+1) + " - " + itoa(hi+1)
}

// pageGroupFirst is the lowest page index a group shows.
func pageGroupFirst(g pageGroup) int {
	if len(g.indexes) == 0 {
		return -1
	}
	lo := g.indexes[0]
	for _, i := range g.indexes[1:] {
		if i < lo {
			lo = i
		}
	}
	return lo
}

// pageGroupLast is the highest page index a group shows.
func pageGroupLast(g pageGroup) int {
	if len(g.indexes) == 0 {
		return -1
	}
	hi := g.indexes[0]
	for _, i := range g.indexes[1:] {
		if i > hi {
			hi = i
		}
	}
	return hi
}

// readerSettings is what the reader remembers between galleries and runs:
// how a page turns and what a turn shows. It replaces the direction, layout
// and wheel flags the reader used to carry as readerPaging.
type readerSettings struct {
	// rtl reads right to left, as Japanese manga do.
	rtl bool
	// double shows two pages at a time (pageTurnMode "spread").
	double bool
	// hasCover has the first page stand alone, so its spread starts at the
	// second page. It is only meaningful in double mode.
	hasCover bool
	// autoIntervalMS is how long a page stays up while auto page turn runs.
	autoIntervalMS int
	// dark sets the reader's own background black rather than white. The
	// reader decides this itself instead of following the app's theme, as
	// comimi's backgroundColor setting does: a manga is read on the tone
	// the reader picks.
	dark bool
}

// defaultReaderSettings is what a fresh install opens with. It follows the
// app's defaults rather than comimi's: right to left for a Japanese manga
// reader, single pages (as the TypeScript reader ran), a cover page, and five
// seconds between auto turns.
func defaultReaderSettings() readerSettings {
	return readerSettings{
		rtl:            true,
		double:         false,
		hasCover:       true,
		autoIntervalMS: 5000,
		dark:           false,
	}
}

// spread reports whether pages pair up where the window is wide enough.
func (s readerSettings) spread() bool {
	return s.double
}

// autoInterval is how long auto page turn leaves a page up.
func (s readerSettings) autoInterval() int {
	if s.autoIntervalMS < 1000 {
		return 5000
	}
	return s.autoIntervalMS
}

// autoTurnRange is the seconds between auto turns the settings panel offers,
// as comimi's slider: 3 to 30 seconds, in steps of one.
const (
	autoTurnMin = 3
	autoTurnMax = 30
)

// normalizeReaderSettings clamps what a stale or hand-edited value could hold.
func normalizeReaderSettings(s readerSettings) readerSettings {
	if s.autoIntervalMS < 3000 {
		s.autoIntervalMS = 5000
	}
	if s.autoIntervalMS > 30000 {
		s.autoIntervalMS = 30000
	}
	return s
}
